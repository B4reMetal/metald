// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"database/sql"
	"fmt"
	"sync"

	_ "modernc.org/sqlite"
)

// ContextDB holds conversation state that must survive a restart: session history, channel recaps
// and the searchable chat log.
type ContextDB struct {
	mu sync.Mutex
	db *sql.DB
}

var (
	globalContextDB     *ContextDB
	globalContextDBOnce sync.Once
	globalContextDBErr  error
)

// Context returns the process-wide context database, opening it on first use.
func Context() (*ContextDB, error) {
	globalContextDBOnce.Do(func() {
		globalContextDB, globalContextDBErr = OpenContextDB(DataPath("context.db"))
	})
	return globalContextDB, globalContextDBErr
}

// OpenContextDB opens (and creates if needed) a context database at path.
func OpenContextDB(path string) (*ContextDB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open context db: %w", err)
	}
	schema := `
	CREATE TABLE IF NOT EXISTS sessions (
		key      TEXT PRIMARY KEY,
		history  TEXT NOT NULL,
		updated  INTEGER NOT NULL
	);
	CREATE TABLE IF NOT EXISTS recaps (
		key      TEXT PRIMARY KEY,
		recap    TEXT NOT NULL,
		updated  INTEGER NOT NULL
	);
	CREATE TABLE IF NOT EXISTS chatlog (
		id    INTEGER PRIMARY KEY AUTOINCREMENT,
		key   TEXT NOT NULL,
		nick  TEXT NOT NULL,
		text  TEXT NOT NULL,
		at    INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_chatlog_key_at ON chatlog(key, at);
	CREATE VIRTUAL TABLE IF NOT EXISTS chatlog_fts USING fts5(
		text, content='chatlog', content_rowid='id', tokenize='porter unicode61');
	CREATE TRIGGER IF NOT EXISTS chatlog_ai AFTER INSERT ON chatlog BEGIN
		INSERT INTO chatlog_fts(rowid, text) VALUES (new.id, new.text);
	END;
	CREATE TRIGGER IF NOT EXISTS chatlog_ad AFTER DELETE ON chatlog BEGIN
		INSERT INTO chatlog_fts(chatlog_fts, rowid, text) VALUES ('delete', old.id, old.text);
	END;
	`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create context schema: %w", err)
	}
	return &ContextDB{db: db}, nil
}

// Close closes the database.
func (c *ContextDB) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.db.Close()
}
