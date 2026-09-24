// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// MemoryStore holds things the bot has been told to remember.
type MemoryStore struct {
	mu sync.Mutex
	db *sql.DB
}

// Memory is one remembered fact.
type Memory struct {
	ID      int64
	Network string // which network learned this; "" predates multi-network
	Subject string // who or what this is about, lowercased for lookup
	Fact    string
	Author  string // who told the bot, for provenance
	Channel string
	Created time.Time
}

var (
	globalMemories     *MemoryStore
	globalMemoriesOnce sync.Once
	globalMemoriesErr  error
)

// Memories returns the process-wide store, opening it on first use.
func Memories() (*MemoryStore, error) {
	globalMemoriesOnce.Do(func() {
		globalMemories, globalMemoriesErr = OpenMemoryStore(DataPath("memories.db"))
	})
	return globalMemories, globalMemoriesErr
}

// OpenMemoryStore opens (and creates if needed) a store at path.
func OpenMemoryStore(path string) (*MemoryStore, error) {
	// WAL so a read while a write is in flight doesn't block, and busy_timeout
	// so concurrent tool calls wait briefly rather than erroring outright.
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open memory store: %w", err)
	}

	schema := `
	CREATE TABLE IF NOT EXISTS memories (
		id       INTEGER PRIMARY KEY AUTOINCREMENT,
		subject  TEXT NOT NULL,
		fact     TEXT NOT NULL,
		author   TEXT NOT NULL DEFAULT '',
		channel  TEXT NOT NULL DEFAULT '',
		created  INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_memories_subject ON memories(subject);
	`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create memory schema: %w", err)
	}

	// Memories are per network, like conversations: one learned on a testbed must not
	// surface on the live network. Added by migration so existing databases upgrade in place.
	if !hasColumn(db, "memories", "network") {
		if _, err := db.Exec(`ALTER TABLE memories ADD COLUMN network TEXT NOT NULL DEFAULT ''`); err != nil {
			db.Close()
			return nil, fmt.Errorf("add network column: %w", err)
		}
	}

	// Uniqueness is per-network: the same fact about the same person on two networks is two memories,
	// because they were learned from two different sets of people.
	if _, err := db.Exec(`
		DROP INDEX IF EXISTS idx_memories_unique;
		CREATE UNIQUE INDEX IF NOT EXISTS idx_memories_unique_net ON memories(network, subject, fact);
		CREATE INDEX IF NOT EXISTS idx_memories_net_subject ON memories(network, subject);
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create memory indexes: %w", err)
	}

	return &MemoryStore{db: db}, nil
}

// hasColumn reports whether a table already has a column, so the migration is
// idempotent across restarts.
func hasColumn(db *sql.DB, table, column string) bool {
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil && n == column {
			return true
		}
	}
	return false
}

// AdoptUnscopedMemories stamps pre-multi-network rows with a network.
func (m *MemoryStore) AdoptUnscopedMemories(network string) (int64, error) {
	if network == "" {
		return 0, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	res, err := m.db.Exec(`UPDATE memories SET network = ? WHERE network = ''`, network)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// normalizeSubject lowercases and trims, so "Dave" and "dave" are one
// subject - IRC nicks are case-insensitive and subjects are usually nicks.
func normalizeSubject(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// Remember stores a fact. Re-remembering the same fact about the same subject
// updates it in place rather than creating a duplicate.
func (m *MemoryStore) Remember(network, subject, fact, author, channel string) (int64, error) {
	subject = normalizeSubject(subject)
	fact = strings.TrimSpace(fact)
	if subject == "" || fact == "" {
		return 0, fmt.Errorf("subject and fact are both required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	res, err := m.db.Exec(
		`INSERT INTO memories (network, subject, fact, author, channel, created)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(network, subject, fact) DO UPDATE SET
		   author = excluded.author, created = excluded.created`,
		network, subject, fact, author, channel, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return id, nil
}

// Recall returns memories about a subject, newest first.
func (m *MemoryStore) Recall(network, subject string, limit int) ([]Memory, error) {
	if limit <= 0 {
		limit = 20
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	rows, err := m.db.Query(
		`SELECT id, network, subject, fact, author, channel, created FROM memories
		 WHERE network = ? AND subject = ? ORDER BY created DESC LIMIT ?`,
		network, normalizeSubject(subject), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMemories(rows)
}

// Search finds memories whose subject OR text matches, for "what do you know
// about X" when X isn't an exact subject.
func (m *MemoryStore) Search(network, term string, limit int) ([]Memory, error) {
	if limit <= 0 {
		limit = 20
	}
	like := "%" + strings.ToLower(strings.TrimSpace(term)) + "%"

	m.mu.Lock()
	defer m.mu.Unlock()

	rows, err := m.db.Query(
		`SELECT id, network, subject, fact, author, channel, created FROM memories
		 WHERE network = ? AND (subject LIKE ? OR lower(fact) LIKE ?)
		 ORDER BY created DESC LIMIT ?`, network, like, like, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMemories(rows)
}

// List returns the most recent memories regardless of subject.
func (m *MemoryStore) List(network string, limit int) ([]Memory, error) {
	if limit <= 0 {
		limit = 50
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	rows, err := m.db.Query(
		`SELECT id, network, subject, fact, author, channel, created FROM memories
		 WHERE network = ? ORDER BY created DESC LIMIT ?`, network, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMemories(rows)
}

// Get returns one memory by id.
func (m *MemoryStore) Get(network string, id int64) (Memory, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var mem Memory
	var created int64
	err := m.db.QueryRow(
		`SELECT id, network, subject, fact, author, channel, created FROM memories
		 WHERE id = ? AND network = ?`,
		id, network).Scan(&mem.ID, &mem.Network, &mem.Subject, &mem.Fact, &mem.Author, &mem.Channel, &created)
	if err == sql.ErrNoRows {
		return Memory{}, false, nil
	}
	if err != nil {
		return Memory{}, false, err
	}
	mem.Created = time.Unix(created, 0)
	return mem, true, nil
}

// Forget deletes one memory by id. Reports false if it wasn't there.
func (m *MemoryStore) Forget(network string, id int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	res, err := m.db.Exec(`DELETE FROM memories WHERE id = ? AND network = ?`, id, network)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ForgetSubject deletes every memory about one subject, returning the count.
func (m *MemoryStore) ForgetSubject(network, subject string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	res, err := m.db.Exec(`DELETE FROM memories WHERE network = ? AND subject = ?`,
		network, normalizeSubject(subject))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// Clear deletes everything, returning how many memories were removed.
func (m *MemoryStore) Clear(network string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	res, err := m.db.Exec(`DELETE FROM memories WHERE network = ?`, network)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// Count returns how many memories are stored.
func (m *MemoryStore) Count(network string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var n int64
	err := m.db.QueryRow(`SELECT COUNT(*) FROM memories WHERE network = ?`, network).Scan(&n)
	return n, err
}

// Close releases the database.
func (m *MemoryStore) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.db.Close()
}

func scanMemories(rows *sql.Rows) ([]Memory, error) {
	var out []Memory
	for rows.Next() {
		var mem Memory
		var created int64
		if err := rows.Scan(&mem.ID, &mem.Network, &mem.Subject, &mem.Fact, &mem.Author, &mem.Channel, &created); err != nil {
			return nil, err
		}
		mem.Created = time.Unix(created, 0)
		out = append(out, mem)
	}
	return out, rows.Err()
}
