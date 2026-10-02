// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexschlessinger/pollytool/messages"
	"github.com/alexschlessinger/pollytool/sessions"
)

func testContextDB(t *testing.T) *ContextDB {
	t.Helper()
	db, err := OpenContextDB(filepath.Join(t.TempDir(), "context.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// A conversation must come back after a restart, with the system prompt taken from current config
// rather than from whatever was saved.
func TestSessionSurvivesRestart(t *testing.T) {
	db := testContextDB(t)
	store := NewPersistentSessionStore(db, &sessions.Metadata{SystemPrompt: "old prompt"})
	s, _ := store.Get("net/#chat")
	s.AddMessage(user("(nick:dave) hello"))
	s.AddMessage(messages.ChatMessage{Role: messages.MessageRoleAssistant, Content: "hi dave",
		ToolCalls: []messages.ChatMessageToolCall{{ID: "1", Name: "slap", Arguments: `{"nick":"dave"}`}}})
	store.Flush()

	restarted := NewPersistentSessionStore(db, &sessions.Metadata{SystemPrompt: "new prompt"})
	s2, _ := restarted.Get("net/#chat")
	h := s2.GetHistory()
	if len(h) != 3 {
		t.Fatalf("history = %d messages, want 3: %+v", len(h), h)
	}
	if h[0].Role != messages.MessageRoleSystem || h[0].Content != "new prompt" {
		t.Errorf("system prompt = %q, want the current one", h[0].Content)
	}
	if h[1].Content != "(nick:dave) hello" || h[2].ToolCalls[0].Name != "slap" {
		t.Errorf("conversation not restored intact: %+v", h[1:])
	}
}

// +reset clears the history; the cleared state must be what a restart sees.
func TestClearedSessionStaysCleared(t *testing.T) {
	db := testContextDB(t)
	store := NewPersistentSessionStore(db, &sessions.Metadata{SystemPrompt: "p"})
	s, _ := store.Get("k")
	s.AddMessage(user("(nick:dave) remember me"))
	store.Flush()
	s.Clear()
	store.Flush()

	s2, _ := NewPersistentSessionStore(db, &sessions.Metadata{SystemPrompt: "p"}).Get("k")
	if n := len(s2.GetHistory()); n != 1 {
		t.Errorf("history after clear+restart = %d messages, want only the system prompt", n)
	}
}

// The store never trims or expires on its own; that is the recap folder's job.
func TestSessionStoreDoesNotTrim(t *testing.T) {
	store := NewPersistentSessionStore(testContextDB(t), &sessions.Metadata{MaxHistoryTokens: 10, TTL: time.Nanosecond})
	s, _ := store.Get("k")
	for i := 0; i < 20; i++ {
		s.AddMessage(user(strings.Repeat("word ", 50)))
	}
	store.Expire()
	s, _ = store.Get("k")
	if n := len(s.GetHistory()); n != 20 {
		t.Errorf("history = %d, want all 20 kept", n)
	}
}

func TestRecapSetGetClear(t *testing.T) {
	db := testContextDB(t)
	if db.Recap("k") != "" {
		t.Fatal("recap before any was written")
	}
	_ = db.SetRecap("k", "first")
	_ = db.SetRecap("k", "second")
	if got := db.Recap("k"); got != "second" {
		t.Errorf("recap = %q", got)
	}
	if !db.ClearRecap("k") || db.Recap("k") != "" {
		t.Error("clear did not remove the recap")
	}
}

func TestBacklogTakeWindowAndMax(t *testing.T) {
	b := &BacklogStore{lines: map[string][]BacklogLine{}}
	now := time.Now()
	b.Add("k", BacklogLine{At: now.Add(-time.Hour), Nick: "old", Text: "stale"}, 10)
	for i := 0; i < 5; i++ {
		b.Add("k", BacklogLine{At: now, Nick: "n", Text: string(rune('a' + i))}, 10)
	}
	b.Add("other", BacklogLine{At: now, Nick: "x", Text: "elsewhere"}, 10)

	got := b.Take("k", 20*time.Minute, 3)
	if len(got) != 3 || got[0].Text != "c" || got[2].Text != "e" {
		t.Fatalf("take = %+v, want the newest 3 inside the window, oldest first", got)
	}
	if again := b.Take("k", 20*time.Minute, 3); len(again) != 0 {
		t.Errorf("lines handed over twice: %+v", again)
	}
	if other := b.Take("other", time.Hour, 10); len(other) != 1 {
		t.Errorf("another channel's backlog was touched: %+v", other)
	}
}

func TestBacklogKeepsAtMostMax(t *testing.T) {
	b := &BacklogStore{lines: map[string][]BacklogLine{}}
	for i := 0; i < 50; i++ {
		b.Add("k", BacklogLine{At: time.Now(), Text: "x"}, 30)
	}
	if n := len(b.lines["k"]); n != 30 {
		t.Errorf("kept %d lines, want 30", n)
	}
	b.Add("k", BacklogLine{At: time.Now(), Text: "x"}, 0)
	if n := len(b.lines["k"]); n != 30 {
		t.Errorf("max 0 should record nothing, have %d", n)
	}
}

// Search is scoped to one conversation: a private chat must never show up in a channel search.
func TestSearchLogScopesToConversation(t *testing.T) {
	db := testContextDB(t)
	now := time.Now()
	_ = db.LogLine("net/#chat", "bob", "the thermite demo was cancelled", now)
	_ = db.LogLine("net/#chat", "carol", "thermite is aluminium and rust", now)
	_ = db.LogLine("net/dave", "dave", "my thermite secret", now)

	got, err := db.SearchLog("net/#chat", "thermite", "", now.Add(-time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("hits = %+v, want the 2 channel lines only", got)
	}
	for _, l := range got {
		if strings.Contains(l.Text, "secret") {
			t.Error("private chat leaked into a channel search")
		}
	}

	byNick, _ := db.SearchLog("net/#chat", "thermite", "Carol", now.Add(-time.Hour), 10)
	if len(byNick) != 1 || byNick[0].Nick != "carol" {
		t.Errorf("nick filter = %+v", byNick)
	}
}

func TestSearchLogHonoursSinceAndPrune(t *testing.T) {
	db := testContextDB(t)
	old := time.Now().AddDate(0, 0, -40)
	_ = db.LogLine("k", "bob", "ancient accordion talk", old)
	_ = db.LogLine("k", "bob", "fresh accordion talk", time.Now())

	got, _ := db.SearchLog("k", "accordion", "", time.Now().AddDate(0, 0, -30), 10)
	if len(got) != 1 || !strings.Contains(got[0].Text, "fresh") {
		t.Errorf("since not applied: %+v", got)
	}
	if n, err := db.PruneLog(time.Now().AddDate(0, 0, -30)); err != nil || n != 1 {
		t.Errorf("prune = %d, %v; want 1 line removed", n, err)
	}
	all, _ := db.SearchLog("k", "accordion", "", time.Time{}, 10)
	if len(all) != 1 {
		t.Errorf("after prune = %+v", all)
	}
}

// Anything a user types must be searched for as words, never parsed as FTS syntax.
func TestFTSQueryQuotesEverything(t *testing.T) {
	db := testContextDB(t)
	_ = db.LogLine("k", "bob", `he said "NEAR(x y)" and OR then AND`, time.Now())
	for _, q := range []string{`"NEAR(x y)"`, `OR AND NOT`, `col:text*`, `"`, `-- ; DROP TABLE chatlog`} {
		if _, err := db.SearchLog("k", q, "", time.Time{}, 5); err != nil {
			t.Errorf("query %q errored: %v", q, err)
		}
	}
	if got := FTSQuery("the and is"); got != "" {
		t.Errorf("stopwords only should give no query, got %q", got)
	}
	if got := FTSQuery(`Thermite "demo"`); got != `"thermite" OR "demo"` {
		t.Errorf("query = %q", got)
	}
}
