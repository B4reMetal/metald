// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestReminderStore(t *testing.T) *ReminderStore {
	t.Helper()
	return NewReminderStore(filepath.Join(t.TempDir(), "reminders.json"))
}

func TestAddAndList(t *testing.T) {
	s := newTestReminderStore(t)

	r, err := s.Add("", "dave", "#chat", "stand up", "BareMetal", 30*time.Minute)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if r.ID == "" {
		t.Fatal("expected a generated id")
	}

	list := s.List("")
	if len(list) != 1 || list[0].Nick != "dave" || list[0].Text != "stand up" {
		t.Fatalf("unexpected list: %+v", list)
	}
}

func TestListIsSortedBySoonestFirst(t *testing.T) {
	s := newTestReminderStore(t)
	s.Add("", "a", "#chat", "later", "x", time.Hour)
	s.Add("", "b", "#chat", "sooner", "x", 5*time.Minute)

	list := s.List("")
	if len(list) != 2 || list[0].Text != "sooner" {
		t.Fatalf("expected soonest first, got %+v", list)
	}
}

func TestCancel(t *testing.T) {
	s := newTestReminderStore(t)
	r, _ := s.Add("", "dave", "#chat", "x", "y", time.Hour)

	if !s.Cancel("", r.ID) {
		t.Fatal("Cancel should report the reminder existed")
	}
	if s.Cancel("", r.ID) {
		t.Fatal("second Cancel should report nothing to remove")
	}
	if len(s.List("")) != 0 {
		t.Fatal("reminder should be gone")
	}
}

func TestCancelUnknownID(t *testing.T) {
	s := newTestReminderStore(t)
	if s.Cancel("", "nope") {
		t.Fatal("cancelling a nonexistent id should return false")
	}
}

// The delay is clamped in the store as well as in the tool - the caps are a
// safety property, so they can't depend on every caller remembering them.
func TestAddClampsDelayToBounds(t *testing.T) {
	s := newTestReminderStore(t)

	long, _ := s.Add("", "a", "#chat", "x", "y", 30*24*time.Hour)
	if d := time.Until(long.Due); d > MaxReminderDelay+time.Minute {
		t.Fatalf("delay not clamped to the ceiling: %v", d)
	}

	short, _ := s.Add("", "b", "#chat", "x", "y", time.Second)
	if d := time.Until(short.Due); d < MinReminderDelay-time.Second {
		t.Fatalf("delay not clamped to the floor: %v", d)
	}
}

func TestPerNickLimit(t *testing.T) {
	s := newTestReminderStore(t)

	for i := 0; i < MaxRemindersPerNick; i++ {
		if _, err := s.Add("", "spammer", "#chat", "x", "y", time.Hour); err != nil {
			t.Fatalf("Add %d should have succeeded: %v", i, err)
		}
	}
	if _, err := s.Add("", "spammer", "#chat", "x", "y", time.Hour); err == nil {
		t.Fatal("expected the per-nick cap to reject the next one")
	}
	// A different nick is unaffected.
	if _, err := s.Add("", "someoneelse", "#chat", "x", "y", time.Hour); err != nil {
		t.Fatalf("a different nick should not be capped: %v", err)
	}
}

// IRC nicks are case-insensitive, so the cap must not be bypassable by
// alternating capitalisation.
func TestPerNickLimitIsCaseInsensitive(t *testing.T) {
	s := newTestReminderStore(t)
	for i := 0; i < MaxRemindersPerNick; i++ {
		s.Add("", "Spammer", "#chat", "x", "y", time.Hour)
	}
	if _, err := s.Add("", "sPaMMeR", "#chat", "x", "y", time.Hour); err == nil {
		t.Fatal("case variation should not bypass the per-nick cap")
	}
}

func TestTotalLimit(t *testing.T) {
	s := newTestReminderStore(t)

	added := 0
	for i := 0; added < MaxRemindersTotal; i++ {
		// Spread across nicks so the per-nick cap isn't what stops us.
		nick := string(rune('a'+i%26)) + string(rune('a'+i/26))
		if _, err := s.Add("", nick, "#chat", "x", "y", time.Hour); err == nil {
			added++
		}
	}
	if _, err := s.Add("", "zzz", "#chat", "x", "y", time.Hour); err == nil {
		t.Fatal("expected the total cap to reject the next one")
	}
}

func TestPopDueReturnsOnlyDue(t *testing.T) {
	s := newTestReminderStore(t)
	s.Add("", "soon", "#chat", "fire me", "y", MinReminderDelay)
	s.Add("", "later", "#chat", "not yet", "y", time.Hour)

	// Look from a point after the first is due but before the second.
	due, stale := s.PopDue(time.Now().Add(2*MinReminderDelay), "")
	if len(stale) != 0 {
		t.Fatalf("nothing should be stale yet: %+v", stale)
	}
	if len(due) != 1 || due[0].Nick != "soon" {
		t.Fatalf("expected exactly the due one, got %+v", due)
	}
	if len(s.List("")) != 1 {
		t.Fatal("the not-yet-due reminder should still be pending")
	}
}

// Popping must consume: a second call for the same instant returns nothing,
// or an overlapping tick would deliver the same reminder twice.
func TestPopDueConsumes(t *testing.T) {
	s := newTestReminderStore(t)
	s.Add("", "a", "#chat", "x", "y", MinReminderDelay)

	at := time.Now().Add(2 * MinReminderDelay)
	if due, _ := s.PopDue(at, ""); len(due) != 1 {
		t.Fatalf("first pop should return it, got %d", len(due))
	}
	if due, _ := s.PopDue(at, ""); len(due) != 0 {
		t.Fatalf("second pop should return nothing, got %d", len(due))
	}
}

// A reminder that came due during a long outage is dropped, not dumped into
// the channel hours later as though it were current.
func TestPopDueDropsStale(t *testing.T) {
	s := newTestReminderStore(t)
	s.Add("", "a", "#chat", "ancient", "y", time.Hour)

	due, stale := s.PopDue(time.Now().Add(time.Hour+MaxReminderLateness+time.Minute), "")
	if len(due) != 0 {
		t.Fatalf("expected nothing deliverable, got %+v", due)
	}
	if len(stale) != 1 || stale[0].Text != "ancient" {
		t.Fatalf("expected it reported as stale, got %+v", stale)
	}
	if len(s.List("")) != 0 {
		t.Fatal("stale reminders should still be removed from the store")
	}
}

func TestPersistsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reminders.json")

	s1 := NewReminderStore(path)
	r, _ := s1.Add("", "dave", "#chat", "survive this", "BareMetal", time.Hour)

	// Simulate a restart.
	s2 := NewReminderStore(path)
	list := s2.List("")
	if len(list) != 1 || list[0].ID != r.ID || list[0].Text != "survive this" {
		t.Fatalf("reminder did not survive restart: %+v", list)
	}
}

func TestMissingFileIsNotAnError(t *testing.T) {
	s := NewReminderStore(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if len(s.List("")) != 0 {
		t.Fatal("a missing file should just mean nothing is scheduled")
	}
}

// Delivery: a sender that fails must not consume the reminder, or a
// disconnected bot would silently eat everything that came due.
func TestFailedDeliveryKeepsReminderPending(t *testing.T) {
	s := newTestReminderStore(t)
	s.Add("", "a", "#chat", "x", "y", MinReminderDelay)

	// Force it due.
	s.mu.Lock()
	for id, r := range s.entries {
		r.Due = time.Now().Add(-time.Second)
		s.entries[id] = r
	}
	s.mu.Unlock()

	deliverDueReminders(s, "", func(channel, message string) bool { return false })

	if len(s.List("")) != 1 {
		t.Fatal("a failed send must leave the reminder pending for a retry")
	}
}

func TestSuccessfulDeliveryFormatsAndConsumes(t *testing.T) {
	s := newTestReminderStore(t)
	s.Add("", "dave", "#chat", "stand up", "BareMetal", MinReminderDelay)

	s.mu.Lock()
	for id, r := range s.entries {
		r.Due = time.Now().Add(-time.Second)
		s.entries[id] = r
	}
	s.mu.Unlock()

	var gotChannel, gotMessage string
	deliverDueReminders(s, "", func(channel, message string) bool {
		gotChannel, gotMessage = channel, message
		return true
	})

	if gotChannel != "#chat" {
		t.Errorf("channel = %q, want #chat", gotChannel)
	}
	if !strings.HasPrefix(gotMessage, "dave: stand up") {
		t.Errorf("message = %q, want it to ping the nick then the text", gotMessage)
	}
	if len(s.List("")) != 0 {
		t.Error("a delivered reminder should be consumed")
	}
}

// A reminder delivered long after it was due says so, rather than reading as
// a current one - the bot restarts often enough for this to matter.
func TestLateDeliveryIsMarked(t *testing.T) {
	s := newTestReminderStore(t)
	s.Add("", "dave", "#chat", "stand up", "BareMetal", MinReminderDelay)

	s.mu.Lock()
	for id, r := range s.entries {
		r.Due = time.Now().Add(-30 * time.Minute)
		s.entries[id] = r
	}
	s.mu.Unlock()

	var gotMessage string
	deliverDueReminders(s, "", func(channel, message string) bool {
		gotMessage = message
		return true
	})

	if !strings.Contains(gotMessage, "late") {
		t.Errorf("expected a lateness note, got %q", gotMessage)
	}
}
