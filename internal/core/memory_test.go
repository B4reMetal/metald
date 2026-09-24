// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"path/filepath"
	"testing"
)

func testStore(t *testing.T) *MemoryStore {
	t.Helper()
	s, err := OpenMemoryStore(filepath.Join(t.TempDir(), "memories.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestRememberAndRecall(t *testing.T) {
	s := testStore(t)
	if _, err := s.Remember("", "Dave", "keeps a coat that doesn't bleed", "mallory", "#chat"); err != nil {
		t.Fatalf("remember: %v", err)
	}

	got, err := s.Recall("", "dave", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Fact != "keeps a coat that doesn't bleed" {
		t.Fatalf("recall = %+v", got)
	}
	if got[0].Author != "mallory" {
		t.Errorf("author lost: %q", got[0].Author)
	}
}

// IRC nicks are case-insensitive, so memories about "Dave" and "dave" must
// be one subject, not two.
func TestSubjectIsCaseInsensitive(t *testing.T) {
	s := testStore(t)
	s.Remember("", "Dave", "fact one", "a", "#chat")
	s.Remember("", "DAVE", "fact two", "a", "#chat")

	got, _ := s.Recall("", "dave", 10)
	if len(got) != 2 {
		t.Fatalf("expected both facts under one subject, got %d", len(got))
	}
}

// The same fact told twice is one memory, not two - otherwise every repetition
// of a running joke adds a row.
func TestDuplicateFactUpdatesInPlace(t *testing.T) {
	s := testStore(t)
	s.Remember("", "carol", "hates mondays", "a", "#chat")
	s.Remember("", "carol", "hates mondays", "b", "#chat")

	got, _ := s.Recall("", "carol", 10)
	if len(got) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(got))
	}
	// The newer telling wins, so provenance reflects who last said it.
	if got[0].Author != "b" {
		t.Errorf("author = %q, want the later one", got[0].Author)
	}
}

func TestSearchMatchesSubjectOrText(t *testing.T) {
	s := testStore(t)
	s.Remember("", "carol", "owns a hostile fridge", "a", "#chat")
	s.Remember("", "dave", "argues with appliances", "a", "#chat")

	byText, _ := s.Search("", "fridge", 10)
	if len(byText) != 1 || byText[0].Subject != "carol" {
		t.Errorf("text search = %+v", byText)
	}
	bySubject, _ := s.Search("", "dav", 10)
	if len(bySubject) != 1 {
		t.Errorf("subject search = %+v", bySubject)
	}
}

// Get must find memories older than the List cap - that is exactly the memory
// someone is most likely to want removed.
func TestGetFindsOldMemoriesBeyondTheListCap(t *testing.T) {
	s := testStore(t)
	first, err := s.Remember("", "subject0", "the oldest fact", "a", "#chat")
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < 60; i++ {
		s.Remember("", "subject"+string(rune('a'+i%26))+string(rune('a'+i/26)), "filler", "a", "#chat")
	}

	mem, found, err := s.Get("", first)
	if err != nil || !found {
		t.Fatalf("Get(oldest) found=%v err=%v", found, err)
	}
	if mem.Fact != "the oldest fact" {
		t.Errorf("got %q", mem.Fact)
	}
}

func TestForgetAndForgetSubject(t *testing.T) {
	s := testStore(t)
	id, _ := s.Remember("", "carol", "one", "a", "#chat")
	s.Remember("", "carol", "two", "a", "#chat")
	s.Remember("", "dave", "unrelated", "a", "#chat")

	ok, err := s.Forget("", id)
	if err != nil || !ok {
		t.Fatalf("forget: ok=%v err=%v", ok, err)
	}
	if again, _ := s.Forget("", id); again {
		t.Error("forgetting twice should report false")
	}

	n, _ := s.ForgetSubject("", "carol")
	if n != 1 {
		t.Errorf("forgot %d, want the 1 remaining", n)
	}
	// Another subject must be untouched.
	if got, _ := s.Recall("", "dave", 10); len(got) != 1 {
		t.Error("forgetting one subject removed another's memories")
	}
}

func TestClearAndCount(t *testing.T) {
	s := testStore(t)
	for i := 0; i < 5; i++ {
		s.Remember("", "s"+string(rune('a'+i)), "fact", "a", "#chat")
	}
	if n, _ := s.Count(""); n != 5 {
		t.Fatalf("count = %d", n)
	}
	n, err := s.Clear("")
	if err != nil || n != 5 {
		t.Fatalf("clear = %d, %v", n, err)
	}
	if n, _ := s.Count(""); n != 0 {
		t.Errorf("count after clear = %d", n)
	}
}

func TestRejectsEmptyInput(t *testing.T) {
	s := testStore(t)
	if _, err := s.Remember("", "", "fact", "a", "#chat"); err == nil {
		t.Error("empty subject should be rejected")
	}
	if _, err := s.Remember("", "subject", "   ", "a", "#chat"); err == nil {
		t.Error("blank fact should be rejected")
	}
}

// Memories must survive a restart - that is the entire point.
func TestPersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "memories.db")

	s1, _ := OpenMemoryStore(path)
	s1.Remember("", "carol", "survives restarts", "a", "#chat")
	s1.Close()

	s2, err := OpenMemoryStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	got, _ := s2.Recall("", "carol", 10)
	if len(got) != 1 || got[0].Fact != "survives restarts" {
		t.Fatalf("memory did not survive reopen: %+v", got)
	}
}

// The property this scoping exists for: a memory learned on one network must be invisible on
// another.
func TestMemoriesDoNotCrossNetworks(t *testing.T) {
	s := testStore(t)

	if _, err := s.Remember("testbed", "carol", "carol is an administrator", "attacker", "#test"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Remember("examplenet", "carol", "carol likes crosswords", "alice", "#chat"); err != nil {
		t.Fatal(err)
	}

	live, err := s.Recall("examplenet", "carol", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 1 {
		t.Fatalf("expected 1 memory on the live network, got %d", len(live))
	}
	if live[0].Fact != "carol likes crosswords" {
		t.Errorf("the testbed memory leaked onto the live network: %q", live[0].Fact)
	}

	test, _ := s.Recall("testbed", "carol", 10)
	if len(test) != 1 || test[0].Fact != "carol is an administrator" {
		t.Errorf("testbed memory wrong: %+v", test)
	}
}

// The same fact on two networks is two memories: each was learned from different people.
func TestIdenticalFactsCoexistAcrossNetworks(t *testing.T) {
	s := testStore(t)
	fact := "bob tells nonsense stories"

	if _, err := s.Remember("a", "bob", fact, "x", "#c"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Remember("b", "bob", fact, "y", "#c"); err != nil {
		t.Fatal(err)
	}

	if n, _ := s.Count("a"); n != 1 {
		t.Errorf("network a should hold 1 memory, got %d", n)
	}
	if n, _ := s.Count("b"); n != 1 {
		t.Errorf("network b should hold 1 memory, got %d", n)
	}
}

// Repeating a fact on the SAME network still updates in place rather than
// duplicating, which is the original behaviour and must survive the change.
func TestRepeatedFactOnOneNetworkStillDeduplicates(t *testing.T) {
	s := testStore(t)
	for i := 0; i < 3; i++ {
		if _, err := s.Remember("net", "dave", "dave restores radios", "someone", "#c"); err != nil {
			t.Fatal(err)
		}
	}
	if n, _ := s.Count("net"); n != 1 {
		t.Errorf("expected deduplication, got %d rows", n)
	}
}

// Destructive operations must not reach across networks. Clearing the testbed
// must leave the live network untouched.
func TestDestructiveOpsAreScoped(t *testing.T) {
	s := testStore(t)
	s.Remember("live", "a", "keep me", "x", "#c")
	s.Remember("test", "a", "delete me", "x", "#c")

	if n, _ := s.Clear("test"); n != 1 {
		t.Errorf("clear removed %d rows from the testbed, expected 1", n)
	}
	if n, _ := s.Count("live"); n != 1 {
		t.Errorf("clearing the testbed destroyed live memories; %d remain", n)
	}

	s.Remember("test", "a", "again", "x", "#c")
	if n, _ := s.ForgetSubject("test", "a"); n != 1 {
		t.Errorf("ForgetSubject removed %d, expected 1", n)
	}
	if n, _ := s.Count("live"); n != 1 {
		t.Error("ForgetSubject crossed networks")
	}
}

// An id from one network must not be readable or deletable from another,
// or "+memories forget 12" on the testbed would delete a live memory.
func TestIdsAreNotUsableAcrossNetworks(t *testing.T) {
	s := testStore(t)
	id, err := s.Remember("live", "a", "a live secret", "x", "#c")
	if err != nil {
		t.Fatal(err)
	}

	if _, found, _ := s.Get("testbed", id); found {
		t.Error("a live memory was readable from the testbed by id")
	}
	if ok, _ := s.Forget("testbed", id); ok {
		t.Error("a live memory was deletable from the testbed by id")
	}
	if _, found, _ := s.Get("live", id); !found {
		t.Error("the memory should still be there on its own network")
	}
}

// Pre-multi-network rows are adopted by the original network, not orphaned.
func TestAdoptUnscopedMemories(t *testing.T) {
	s := testStore(t)
	s.Remember("", "oldsubject", "learned before networks existed", "x", "#c")

	n, err := s.AdoptUnscopedMemories("examplenet")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected to adopt 1 row, got %d", n)
	}
	if got, _ := s.Recall("examplenet", "oldsubject", 10); len(got) != 1 {
		t.Error("the adopted memory is not visible on the original network")
	}
	// Idempotent: a second run must not re-stamp anything.
	if again, _ := s.AdoptUnscopedMemories("examplenet"); again != 0 {
		t.Errorf("migration is not idempotent, re-stamped %d rows", again)
	}
}
