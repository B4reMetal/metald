// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *IgnoreStore {
	t.Helper()
	return NewIgnoreStore(filepath.Join(t.TempDir(), "ignores.json"))
}

func TestIgnoreAddAndCheck(t *testing.T) {
	s := newTestStore(t)

	if s.IsIgnored("", "mallory") {
		t.Fatal("nick should not be ignored before being added")
	}
	s.Add("", "mallory", time.Hour)
	if !s.IsIgnored("", "mallory") {
		t.Fatal("nick should be ignored after Add")
	}
}

// IRC nicks are case-insensitive, so an ignore must apply regardless of how
// the nick is capitalised in the incoming message.
func TestIgnoreIsCaseInsensitive(t *testing.T) {
	s := newTestStore(t)
	s.Add("", "Dave", time.Hour)

	for _, variant := range []string{"Dave", "dave", "DAVE", "dAvE"} {
		if !s.IsIgnored("", variant) {
			t.Errorf("expected %q to be ignored", variant)
		}
	}
}

func TestIgnoreExpires(t *testing.T) {
	s := newTestStore(t)
	s.Add("", "temp", 20*time.Millisecond)

	if !s.IsIgnored("", "temp") {
		t.Fatal("should be ignored immediately after Add")
	}
	time.Sleep(40 * time.Millisecond)
	if s.IsIgnored("", "temp") {
		t.Fatal("should no longer be ignored after expiry")
	}
	if len(s.List("")) != 0 {
		t.Fatal("expired entry should be purged from List")
	}
}

func TestIgnoreRemove(t *testing.T) {
	s := newTestStore(t)
	s.Add("", "gone", time.Hour)

	if !s.Remove("", "gone") {
		t.Fatal("Remove should report true for an existing entry")
	}
	if s.IsIgnored("", "gone") {
		t.Fatal("should not be ignored after Remove")
	}
	if s.Remove("", "gone") {
		t.Fatal("Remove should report false for a missing entry")
	}
}

// Removing by a different capitalisation than it was added with must work,
// same normalisation as the lookup path.
func TestIgnoreRemoveIsCaseInsensitive(t *testing.T) {
	s := newTestStore(t)
	s.Add("", "Dave", time.Hour)
	if !s.Remove("", "DAVE") {
		t.Fatal("Remove should match case-insensitively")
	}
	if s.IsIgnored("", "dave") {
		t.Fatal("should not be ignored after case-insensitive Remove")
	}
}

// The bot restarts on every config change, so a day-long ignore has to
// survive a process restart or it is worse than useless.
func TestIgnorePersistsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ignores.json")

	first := NewIgnoreStore(path)
	first.Add("", "mallory", 24*time.Hour)

	second := NewIgnoreStore(path)
	if !second.IsIgnored("", "mallory") {
		t.Fatal("ignore should survive a restart")
	}
}

// An entry that expired while the process was down must not come back.
func TestExpiredIgnoreNotReloaded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ignores.json")

	first := NewIgnoreStore(path)
	first.Add("", "brief", 15*time.Millisecond)
	time.Sleep(35 * time.Millisecond)

	second := NewIgnoreStore(path)
	if second.IsIgnored("", "brief") {
		t.Fatal("expired entry should not be restored on load")
	}
}

func TestIgnoreListSorted(t *testing.T) {
	s := newTestStore(t)
	s.Add("", "zeta", time.Hour)
	s.Add("", "alpha", time.Hour)

	entries := s.List("")
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Nick != "alpha" || entries[1].Nick != "zeta" {
		t.Fatalf("expected sorted order, got %s then %s", entries[0].Nick, entries[1].Nick)
	}
}

// Re-adding should extend/replace rather than duplicate.
func TestIgnoreReAddReplaces(t *testing.T) {
	s := newTestStore(t)
	s.Add("", "dup", time.Minute)
	s.Add("", "dup", 2*time.Hour)

	entries := s.List("")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after re-add, got %d", len(entries))
	}
	if time.Until(entries[0].Expiry) < time.Hour {
		t.Fatal("re-add should have replaced the shorter expiry")
	}
}
