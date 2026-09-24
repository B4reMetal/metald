// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"context"
	"sync"
)

// InFlight tracks running requests so they can be stopped.
type InFlight struct {
	mu     sync.Mutex
	byKey  map[string][]*entry
	nextID uint64
}

type entry struct {
	id        uint64
	requestID string // the bot's own request id, for logging and self-exclusion
	source    string // the nick whose message started this request
	cancel    context.CancelFunc
}

var (
	globalInFlight     *InFlight
	globalInFlightOnce sync.Once
)

// Requests returns the process-wide in-flight registry.
func Requests() *InFlight {
	globalInFlightOnce.Do(func() {
		globalInFlight = &InFlight{byKey: make(map[string][]*entry)}
	})
	return globalInFlight
}

// Track registers a running request and returns a function to deregister it.
// The returned func must be deferred by the caller.
func (f *InFlight) Track(key, requestID, source string, cancel context.CancelFunc) func() {
	f.mu.Lock()
	f.nextID++
	e := &entry{id: f.nextID, requestID: requestID, source: source, cancel: cancel}
	f.byKey[key] = append(f.byKey[key], e)
	f.mu.Unlock()

	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		remaining := f.byKey[key][:0]
		for _, cur := range f.byKey[key] {
			if cur.id != e.id {
				remaining = append(remaining, cur)
			}
		}
		if len(remaining) == 0 {
			delete(f.byKey, key)
			return
		}
		f.byKey[key] = remaining
	}
}

// CancelKey stops every request running for a conversation EXCEPT the one asking.
func (f *InFlight) CancelKey(key, exceptRequestID string) int {
	f.mu.Lock()
	var targets []*entry
	for _, e := range f.byKey[key] {
		if exceptRequestID != "" && e.requestID == exceptRequestID {
			continue
		}
		targets = append(targets, e)
	}
	f.mu.Unlock()

	ids := make([]string, 0, len(targets))
	for _, e := range targets {
		e.cancel()
		ids = append(ids, e.requestID)
	}
	if len(ids) > 0 {
		// Logged by id so "what did that actually stop?" is answerable from
		// the log instead of inferred from a count.
		GetLogger().Info("requests_cancelled", "lock_key", key, "requests", ids)
	}
	return len(targets)
}

// CancelSource stops every request started by one nick, across conversations, EXCEPT the one
// asking.
func (f *InFlight) CancelSource(source, exceptRequestID string) int {
	target := normalizeNick(source)

	f.mu.Lock()
	var matched []*entry
	for _, entries := range f.byKey {
		for _, e := range entries {
			if exceptRequestID != "" && e.requestID == exceptRequestID {
				continue
			}
			if normalizeNick(e.source) == target {
				matched = append(matched, e)
			}
		}
	}
	f.mu.Unlock()

	ids := make([]string, 0, len(matched))
	for _, e := range matched {
		e.cancel()
		ids = append(ids, e.requestID)
	}
	if len(ids) > 0 {
		GetLogger().Info("requests_cancelled", "source", source, "requests", ids)
	}
	return len(matched)
}

// Count reports how many requests are running for a conversation.
func (f *InFlight) Count(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.byKey[key])
}
