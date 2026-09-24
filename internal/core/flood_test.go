// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"testing"
	"time"
)

func TestFloodCountsWithinWindow(t *testing.T) {
	f := NewFloodTracker()
	for i := 1; i <= 4; i++ {
		if got := f.Record("", "spammer", time.Minute); got != i {
			t.Fatalf("message %d: got count %d, want %d", i, got, i)
		}
	}
}

// Messages older than the window must not count toward the rate.
func TestFloodAgesOutOldMessages(t *testing.T) {
	f := NewFloodTracker()
	window := 40 * time.Millisecond

	f.Record("", "slowpoke", window)
	f.Record("", "slowpoke", window)
	time.Sleep(60 * time.Millisecond)

	if got := f.Record("", "slowpoke", window); got != 1 {
		t.Fatalf("expected aged-out history, got count %d", got)
	}
}

// Nicks are tracked independently - one user flooding must not implicate
// anyone else.
func TestFloodIsPerNick(t *testing.T) {
	f := NewFloodTracker()
	for range 5 {
		f.Record("", "loud", time.Minute)
	}
	if got := f.Record("", "quiet", time.Minute); got != 1 {
		t.Fatalf("quiet nick should be unaffected, got count %d", got)
	}
}

// IRC nicks are case-insensitive, so someone can't evade the counter by
// changing capitalisation mid-flood.
func TestFloodIsCaseInsensitive(t *testing.T) {
	f := NewFloodTracker()
	f.Record("", "Dave", time.Minute)
	f.Record("", "dave", time.Minute)
	if got := f.Record("", "DAVE", time.Minute); got != 3 {
		t.Fatalf("expected case-insensitive counting, got %d", got)
	}
}

// After a timeout the offender should start clean, not re-trip instantly on
// their first message back.
func TestFloodResetClearsHistory(t *testing.T) {
	f := NewFloodTracker()
	for range 5 {
		f.Record("", "tripped", time.Minute)
	}
	f.Reset("", "tripped")
	if got := f.Record("", "tripped", time.Minute); got != 1 {
		t.Fatalf("expected clean slate after Reset, got count %d", got)
	}
}

func TestFloodSweepBoundsMemory(t *testing.T) {
	f := NewFloodTracker()
	window := 20 * time.Millisecond

	for i := range maxTrackedNicks + 50 {
		f.Record("", string(rune('a'+i%26))+string(rune('a'+i/26)), window)
	}
	time.Sleep(40 * time.Millisecond)
	// This Record trips the sweep, which should drop every aged-out nick.
	f.Record("", "trigger", window)

	f.mu.Lock()
	size := len(f.events)
	f.mu.Unlock()
	if size > maxTrackedNicks {
		t.Fatalf("tracker grew unbounded: %d entries", size)
	}
}
