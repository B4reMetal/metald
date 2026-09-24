// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package llm

import (
	"sync"
	"testing"

	"github.com/alexschlessinger/pollytool/messages"
	"github.com/alexschlessinger/pollytool/sessions"
)

func newSession(t *testing.T) sessions.Session {
	t.Helper()
	s, err := sessions.NewSyncMapSessionStore(&sessions.Metadata{}).Get("test")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func user(s string) messages.ChatMessage {
	return messages.ChatMessage{Role: messages.MessageRoleUser, Content: s}
}

func asst(s string) messages.ChatMessage {
	return messages.ChatMessage{Role: messages.MessageRoleAssistant, Content: s}
}

// Two overlapping requests: B finishes first. Each question must sit
// directly before its own answer, in completion order.
func TestOverlappingExchangesCommitAsBlocks(t *testing.T) {
	s := newSession(t)
	reqA, reqB := &CompletionRequest{}, &CompletionRequest{}
	setPending(reqA, user("(nick:alice) question A"))
	setPending(reqB, user("(nick:bob) question B"))

	commitExchange(s, append(takePending(reqB), asst("answer B")))
	commitExchange(s, append(takePending(reqA), asst("answer A")))

	var got []string
	for _, m := range s.GetHistory() {
		if m.Role != messages.MessageRoleSystem {
			got = append(got, m.Content)
		}
	}
	want := []string{"(nick:bob) question B", "answer B", "(nick:alice) question A", "answer A"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("position %d: got %q, want %q (full %v)", i, got[i], want[i], got)
		}
	}
}

// The pending question is taken exactly once: whoever commits first owns it.
func TestPendingTakenOnce(t *testing.T) {
	req := &CompletionRequest{}
	setPending(req, user("q"))
	if len(takePending(req)) != 1 || len(takePending(req)) != 0 {
		t.Fatal("pending message must be handed out exactly once")
	}
}

// Many concurrent commits never interleave inside a block.
func TestConcurrentCommitsStayContiguous(t *testing.T) {
	s := newSession(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tag := string(rune('a' + i))
			commitExchange(s, []messages.ChatMessage{user(tag), asst(tag), asst(tag)})
		}(i)
	}
	wg.Wait()
	var h []messages.ChatMessage
	for _, m := range s.GetHistory() {
		if m.Role != messages.MessageRoleSystem {
			h = append(h, m)
		}
	}
	if len(h) != 60 {
		t.Fatalf("want 60 messages, got %d", len(h))
	}
	for i := 0; i < len(h); i += 3 {
		if h[i].Role != messages.MessageRoleUser || h[i+1].Content != h[i].Content || h[i+2].Content != h[i].Content {
			t.Fatalf("block at %d interleaved: %q %q %q", i, h[i].Content, h[i+1].Content, h[i+2].Content)
		}
	}
}
