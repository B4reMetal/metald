// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"sync"
	"time"
)

// BacklogLine is one channel line the bot was not addressed in.
type BacklogLine struct {
	At     time.Time
	Nick   string
	Text   string
	Action bool // a /me
}

// BacklogStore keeps each channel's recent unaddressed lines until the next request there uses them.
type BacklogStore struct {
	mu    sync.Mutex
	lines map[string][]BacklogLine
}

var globalBacklog = &BacklogStore{lines: map[string][]BacklogLine{}}

// Backlog returns the process-wide backlog.
func Backlog() *BacklogStore { return globalBacklog }

// Add records a line, keeping at most max per channel.
func (b *BacklogStore) Add(key string, line BacklogLine, max int) {
	if max <= 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	l := append(b.lines[key], line)
	if len(l) > max {
		l = l[len(l)-max:]
	}
	b.lines[key] = l
}

// Take returns the channel's lines from the last window, at most max, oldest first, and empties the
// channel's backlog so no line is handed over twice.
func (b *BacklogStore) Take(key string, window time.Duration, max int) []BacklogLine {
	b.mu.Lock()
	defer b.mu.Unlock()
	l := b.lines[key]
	delete(b.lines, key)
	cutoff := time.Now().Add(-window)
	var out []BacklogLine
	for _, line := range l {
		if window <= 0 || !line.At.Before(cutoff) {
			out = append(out, line)
		}
	}
	if max > 0 && len(out) > max {
		out = out[len(out)-max:]
	}
	return out
}

// Clear empties one channel's backlog.
func (b *BacklogStore) Clear(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.lines, key)
}
