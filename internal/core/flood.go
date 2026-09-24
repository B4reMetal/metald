// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"sync"
	"time"
)

// FloodTracker counts recent messages per nick over a sliding window, so a user firing many
// messages in quick succession can be auto-timed-out without waiting on the model's judgement.
type FloodTracker struct {
	mu     sync.Mutex
	events map[string][]time.Time
}

var (
	globalFlood     *FloodTracker
	globalFloodOnce sync.Once
)

// Flood returns the process-wide flood tracker.
func Flood() *FloodTracker {
	globalFloodOnce.Do(func() {
		globalFlood = NewFloodTracker()
	})
	return globalFlood
}

func NewFloodTracker() *FloodTracker {
	return &FloodTracker{events: make(map[string][]time.Time)}
}

// maxTrackedNicks bounds memory if a channel sees a lot of distinct nicks.
// Past this, nicks with no activity inside the window are swept.
const maxTrackedNicks = 512

// Record notes a message from nick and returns how many messages that nick
// has sent within the trailing window (including this one).
func (f *FloodTracker) Record(network, nick string, window time.Duration) int {
	nick = ScopeKey(network, nick)
	key := normalizeNick(nick)
	now := time.Now()
	cutoff := now.Add(-window)

	f.mu.Lock()
	defer f.mu.Unlock()

	// Drop timestamps that have aged out, then append this one.
	kept := f.events[key][:0]
	for _, ts := range f.events[key] {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	kept = append(kept, now)
	f.events[key] = kept

	if len(f.events) > maxTrackedNicks {
		f.sweepLocked(cutoff)
	}
	return len(kept)
}

// Reset clears a nick's history, so a timeout starts them from a clean slate
// rather than re-tripping immediately on their first message back.
func (f *FloodTracker) Reset(network, nick string) {
	nick = ScopeKey(network, nick)
	key := normalizeNick(nick)
	f.mu.Lock()
	delete(f.events, key)
	f.mu.Unlock()
}

// sweepLocked drops nicks with no activity inside the window. Caller holds mu.
func (f *FloodTracker) sweepLocked(cutoff time.Time) {
	for nick, timestamps := range f.events {
		live := false
		for _, ts := range timestamps {
			if ts.After(cutoff) {
				live = true
				break
			}
		}
		if !live {
			delete(f.events, nick)
		}
	}
}
