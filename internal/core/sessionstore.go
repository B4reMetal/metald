// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/alexschlessinger/pollytool/messages"
	"github.com/alexschlessinger/pollytool/sessions"
)

// sessionFlushInterval bounds how much conversation a crash can lose.
const sessionFlushInterval = 2 * time.Second

// PersistentSessionStore keeps pollytool's in-memory sessions and writes each changed history to the
// context database, so a restart resumes every conversation where it stopped. Sessions never expire
// or trim here: the recap compactor decides what leaves the history.
type PersistentSessionStore struct {
	inner    sessions.SessionStore
	db       *ContextDB
	wrappers sync.Map // key -> *PersistentSession
	dirty    sync.Map // key -> struct{}
}

// PersistentSession is a session whose changes are queued for writing.
type PersistentSession struct {
	sessions.Session
	key   string
	store *PersistentSessionStore
}

// NewPersistentSessionStore wraps an in-memory store around db. defaults.TTL and
// defaults.MaxHistoryTokens are ignored.
func NewPersistentSessionStore(db *ContextDB, defaults *sessions.Metadata) *PersistentSessionStore {
	d := *defaults
	d.TTL = 0
	d.MaxHistoryTokens = 0
	return &PersistentSessionStore{inner: sessions.NewSyncMapSessionStore(&d), db: db}
}

// Get returns the session for key, restoring its saved history the first time it is asked for.
func (s *PersistentSessionStore) Get(key string) (sessions.Session, error) {
	if w, ok := s.wrappers.Load(key); ok {
		sess := w.(*PersistentSession)
		// Touch the inner session so its last-used time moves.
		if _, err := s.inner.Get(key); err != nil {
			return nil, err
		}
		return sess, nil
	}

	inner, err := s.inner.Get(key)
	if err != nil {
		return nil, err
	}
	w := &PersistentSession{Session: inner, key: key, store: s}
	actual, loaded := s.wrappers.LoadOrStore(key, w)
	if loaded {
		return actual.(*PersistentSession), nil
	}

	for _, m := range s.load(key) {
		inner.AddMessage(m)
	}
	return w, nil
}

// load reads a saved history, or nothing if there is none or it will not parse.
func (s *PersistentSessionStore) load(key string) []messages.ChatMessage {
	s.db.mu.Lock()
	var raw string
	err := s.db.db.QueryRow(`SELECT history FROM sessions WHERE key = ?`, key).Scan(&raw)
	s.db.mu.Unlock()
	if err != nil {
		return nil
	}
	var msgs []messages.ChatMessage
	if err := json.Unmarshal([]byte(raw), &msgs); err != nil {
		slog.Warn("session_restore_failed", "key", key, "error", err.Error())
		return nil
	}
	slog.Info("session_restored", "key", key, "messages", len(msgs))
	return msgs
}

// Delete forgets a session in memory and on disk.
func (s *PersistentSessionStore) Delete(key string) {
	s.inner.Delete(key)
	s.wrappers.Delete(key)
	s.dirty.Delete(key)
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	if _, err := s.db.db.Exec(`DELETE FROM sessions WHERE key = ?`, key); err != nil {
		slog.Warn("session_delete_failed", "key", key, "error", err.Error())
	}
}

// Range visits the wrapped sessions.
func (s *PersistentSessionStore) Range(f func(key, value any) bool) { s.wrappers.Range(f) }

// Expire does nothing: idle conversations are folded into the recap instead of deleted.
func (s *PersistentSessionStore) Expire() {}

func (s *PersistentSessionStore) List() ([]string, error) { return s.inner.List() }
func (s *PersistentSessionStore) Exists(key string) bool  { return s.inner.Exists(key) }
func (s *PersistentSessionStore) GetAllMetadata() map[string]*sessions.Metadata {
	return s.inner.GetAllMetadata()
}
func (s *PersistentSessionStore) GetLast() string { return s.inner.GetLast() }

// AddMessage appends and queues the history for writing.
func (p *PersistentSession) AddMessage(m messages.ChatMessage) {
	p.Session.AddMessage(m)
	p.store.dirty.Store(p.key, struct{}{})
}

// Clear empties the history and queues the empty history for writing.
func (p *PersistentSession) Clear() {
	p.Session.Clear()
	p.store.dirty.Store(p.key, struct{}{})
}

// Key is the store key this session is saved under.
func (p *PersistentSession) Key() string { return p.key }

// Flush writes every changed session now.
func (s *PersistentSessionStore) Flush() {
	s.dirty.Range(func(k, _ any) bool {
		key := k.(string)
		s.dirty.Delete(key)
		w, ok := s.wrappers.Load(key)
		if !ok {
			return true
		}
		s.save(key, w.(*PersistentSession).GetHistory())
		return true
	})
}

// save writes one history, leaving out the system prompt, which always comes from current config.
func (s *PersistentSessionStore) save(key string, history []messages.ChatMessage) {
	var convo []messages.ChatMessage
	for _, m := range history {
		if m.Role != messages.MessageRoleSystem {
			convo = append(convo, m)
		}
	}
	raw, err := json.Marshal(convo)
	if err != nil {
		slog.Warn("session_save_failed", "key", key, "error", err.Error())
		return
	}
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	if _, err := s.db.db.Exec(
		`INSERT INTO sessions(key, history, updated) VALUES(?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET history = excluded.history, updated = excluded.updated`,
		key, string(raw), time.Now().Unix()); err != nil {
		slog.Warn("session_save_failed", "key", key, "error", err.Error())
	}
}

// Run flushes changed sessions until ctx ends, then flushes once more.
func (s *PersistentSessionStore) Run(ctx context.Context) {
	t := time.NewTicker(sessionFlushInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			s.Flush()
			return
		case <-t.C:
			s.Flush()
		}
	}
}
