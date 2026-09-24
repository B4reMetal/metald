// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// IgnoreStore tracks temporarily ignored nicks.
type IgnoreStore struct {
	mu      sync.RWMutex
	path    string
	entries map[string]time.Time // lowercased nick -> expiry
}

var (
	globalIgnores     *IgnoreStore
	globalIgnoresOnce sync.Once
)

// Ignores returns the process-wide ignore store, loading persisted state on
// first use.
func Ignores() *IgnoreStore {
	globalIgnoresOnce.Do(func() {
		globalIgnores = NewIgnoreStore(DataPath("ignores.json"))
	})
	return globalIgnores
}

// NewIgnoreStore creates a store backed by path, loading any existing state.
func NewIgnoreStore(path string) *IgnoreStore {
	s := &IgnoreStore{path: path, entries: make(map[string]time.Time)}
	s.load()
	return s
}

// normalizeNick lowercases for comparison - IRC nicks are case-insensitive,
// so "Dave" and "dave" must be the same entry.
func normalizeNick(nick string) string {
	return strings.ToLower(strings.TrimSpace(nick))
}

// Add ignores nick for d, replacing any existing entry. Returns the expiry.
func (s *IgnoreStore) Add(network, nick string, d time.Duration) time.Time {
	nick = ScopeKey(network, nick)
	expiry := time.Now().Add(d)
	s.mu.Lock()
	s.entries[normalizeNick(nick)] = expiry
	s.mu.Unlock()
	s.save()
	return expiry
}

// Remove un-ignores nick. Returns false if it wasn't ignored.
func (s *IgnoreStore) Remove(network, nick string) bool {
	nick = ScopeKey(network, nick)
	key := normalizeNick(nick)
	s.mu.Lock()
	_, existed := s.entries[key]
	delete(s.entries, key)
	s.mu.Unlock()
	if existed {
		s.save()
	}
	return existed
}

// IsIgnored reports whether nick is currently ignored, purging the entry if
// it has expired.
func (s *IgnoreStore) IsIgnored(network, nick string) bool {
	nick = ScopeKey(network, nick)
	key := normalizeNick(nick)

	s.mu.RLock()
	expiry, ok := s.entries[key]
	s.mu.RUnlock()
	if !ok {
		return false
	}
	if time.Now().Before(expiry) {
		return true
	}

	// Expired - drop it so the list stays clean.
	s.mu.Lock()
	if cur, still := s.entries[key]; still && !time.Now().Before(cur) {
		delete(s.entries, key)
		s.mu.Unlock()
		s.save()
		return false
	}
	s.mu.Unlock()
	return false
}

// IgnoreEntry is one active ignore, for listing.
type IgnoreEntry struct {
	Nick   string
	Expiry time.Time
}

// List returns the currently-active ignores, sorted by nick, dropping any it has expired.
func (s *IgnoreStore) List(network string) []IgnoreEntry {
	now := time.Now()
	var out []IgnoreEntry
	var expired []string

	s.mu.RLock()
	for nick, expiry := range s.entries {
		if !now.Before(expiry) {
			expired = append(expired, nick)
			continue
		}
		// This network only: the testbed must not disclose who is muted on the live network.
		if keyInNetwork(network, nick) {
			out = append(out, IgnoreEntry{Nick: UnscopeKey(network, nick), Expiry: expiry})
		}
	}
	s.mu.RUnlock()

	if len(expired) > 0 {
		s.mu.Lock()
		for _, nick := range expired {
			if cur, ok := s.entries[nick]; ok && !now.Before(cur) {
				delete(s.entries, nick)
			}
		}
		s.mu.Unlock()
		s.save()
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Nick < out[j].Nick })
	return out
}

// load reads persisted state. A missing or unreadable file is not an error -
// it just means nothing is ignored yet.
func (s *IgnoreStore) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var raw map[string]time.Time
	if err := json.Unmarshal(data, &raw); err != nil {
		GetLogger().Warn("ignore_store_unreadable", "path", s.path, "error", err.Error())
		return
	}
	now := time.Now()
	for nick, expiry := range raw {
		if now.Before(expiry) {
			s.entries[normalizeNick(nick)] = expiry
		}
	}
}

// save writes state atomically (temp file + rename) so a crash mid-write
// can't leave a truncated file that loses every active ignore.
func (s *IgnoreStore) save() {
	s.mu.RLock()
	snapshot := make(map[string]time.Time, len(s.entries))
	for nick, expiry := range s.entries {
		snapshot[nick] = expiry
	}
	s.mu.RUnlock()

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		GetLogger().Error("ignore_store_marshal_failed", "error", err.Error())
		return
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		GetLogger().Error("ignore_store_write_failed", "path", tmp, "error", err.Error())
		return
	}
	if err := os.Rename(tmp, s.path); err != nil {
		GetLogger().Error("ignore_store_rename_failed", "path", s.path, "error", err.Error())
		_ = os.Remove(tmp)
		return
	}
	_ = filepath.Clean(s.path)
}
