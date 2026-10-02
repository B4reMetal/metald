// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package llm

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/alexschlessinger/pollytool/llm"
	"github.com/alexschlessinger/pollytool/messages"
	"github.com/alexschlessinger/pollytool/tools"

	"B4reMetal/metald/internal/irc"
	mocktest "B4reMetal/metald/internal/testing"
)

// Mangled names seen in the live log, and what they must resolve to.
func TestResolveToolName(t *testing.T) {
	names := []string{"imagegen__picture", "musicgen__song", "websearch__web_search", "webfetch__fetch",
		"vision__view_image", "tts__speak", "slap__slap", "context7__query-docs", "context7__resolve-library-id"}
	cases := map[string]string{
		"imagegen__picture":  "imagegen__picture",
		"imagegen__Picture":  "imagegen__picture",
		"musicgen__sonG":     "musicgen__song",
		"websearch":          "websearch__web_search",
		"websearch__":        "websearch__web_search",
		"imagegen__":         "imagegen__picture",
		"vision":             "vision__view_image",
		"tts":                "tts__speak",
		"slap":               "slap__slap",
		"web_search":         "websearch__web_search",
		"context7":           "context7", // two tools: ambiguous, left alone
		"nonexistent__thing": "nonexistent__thing",
	}
	for in, want := range cases {
		if got := resolveToolName(names, in); got != want {
			t.Errorf("resolveToolName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestToolDeniedAfterTwoRefusals(t *testing.T) {
	ctx := mocktest.NewMockContext()
	h := newCallbackHandler(ctx, irc.NewChunker(make(chan string, 10), 350), ctx.GetConfig())
	call := messages.ChatMessageToolCall{Name: "sandbox__run_code"}
	for i := 0; i < 2; i++ {
		if ok := h.approveToolCalls([]messages.ChatMessageToolCall{call}); !ok[0] {
			t.Fatalf("denied before refusing (attempt %d)", i)
		}
		h.onToolEnd(call, "Error: refused - no processes", 0, nil)
	}
	ok := h.approveToolCalls([]messages.ChatMessageToolCall{call, {Name: "paste__post_gist"}})
	if ok[0] || !ok[1] {
		t.Errorf("approvals = %v, want the twice-refused tool denied and the other allowed", ok)
	}
}

// scriptedLLM answers each model call with the next scripted reply.
type scriptedLLM struct {
	mu      sync.Mutex
	replies []string
	seen    []*llm.CompletionRequest
}

func (s *scriptedLLM) ChatCompletionStream(_ context.Context, req *llm.CompletionRequest, _ llm.EventStreamProcessor) <-chan *messages.StreamEvent {
	s.mu.Lock()
	reply := s.replies[0]
	s.replies = s.replies[1:]
	s.seen = append(s.seen, req)
	s.mu.Unlock()
	ch := make(chan *messages.StreamEvent, 2)
	ch <- &messages.StreamEvent{Type: messages.EventTypeContent, Content: reply}
	ch <- &messages.StreamEvent{Type: messages.EventTypeComplete,
		Message: &messages.ChatMessage{Role: messages.MessageRoleAssistant, Content: reply}}
	close(ch)
	return ch
}

// The live failure: the model wrote a tool call as text with a cut-off name, so nothing ran and
// nothing was posted. One retry, told the exact names, must produce the answer.
func TestLeakedToolCallIsRetriedOnce(t *testing.T) {
	sys := mocktest.NewMockSystem()
	session, _ := sys.SessionStore.Get("net/#retry")
	ctx := mocktest.NewMockContext().WithSystem(sys).WithSession(session)
	fake := &scriptedLLM{replies: []string{
		"<tool_call>\n<function=imagegen__\n<parameter=prompt>\na robot\n</parameter>\n</function>\n</tool_call>",
		"here is your answer",
	}}
	p := &PollyLLM{client: fake}
	req := NewCompletionRequest(ctx.GetConfig(), session, nil)
	req.Messages = append(req.Messages, messages.ChatMessage{Role: messages.MessageRoleUser, Content: "(nick:dave) draw a robot"})
	setPending(req, req.Messages[len(req.Messages)-1])

	var out []string
	for line := range p.ChatCompletionStream(ctx, req) {
		out = append(out, line)
	}
	if len(fake.seen) != 2 {
		t.Fatalf("model calls = %d, want 2 (original + one retry)", len(fake.seen))
	}
	note := fake.seen[1].Messages[len(fake.seen[1].Messages)-1].Content
	if !strings.Contains(note, "exact names") {
		t.Errorf("retry did not carry the note: %q", note)
	}
	if got := strings.Join(out, "\n"); !strings.Contains(got, "here is your answer") || strings.Contains(got, "tool_call") {
		t.Errorf("channel saw %q", got)
	}
	for _, m := range session.GetHistory() {
		if strings.Contains(m.Content, "exact names") || strings.Contains(m.Content, "<tool_call>") {
			t.Errorf("retry scaffolding committed to history: %q", m.Content)
		}
	}
}

func runScripted(t *testing.T, key string, replies ...string) (*scriptedLLM, []string) {
	t.Helper()
	sys := mocktest.NewMockSystem()
	session, _ := sys.SessionStore.Get(key)
	ctx := mocktest.NewMockContext().WithSystem(sys).WithSession(session)
	fake := &scriptedLLM{replies: replies}
	req := NewCompletionRequest(ctx.GetConfig(), session, nil)
	req.Messages = append(req.Messages, messages.ChatMessage{Role: messages.MessageRoleUser, Content: "(nick:dave) who is my avatar?"})
	setPending(req, req.Messages[len(req.Messages)-1])
	var out []string
	for line := range (&PollyLLM{client: fake}).ChatCompletionStream(ctx, req) {
		out = append(out, line)
	}
	return fake, out
}

// The live case: the model thought for 7,800 tokens and wrote nothing. One nudge gets the answer.
func TestEmptyReplyIsRetriedOnce(t *testing.T) {
	fake, out := runScripted(t, "net/#empty1", "", "a blue-haired shrine maiden")
	if len(fake.seen) != 2 {
		t.Fatalf("model calls = %d, want 2", len(fake.seen))
	}
	if got := strings.Join(out, "\n"); !strings.Contains(got, "shrine maiden") || strings.Contains(got, noAnswer) {
		t.Errorf("channel saw %q", got)
	}
}

// Silent twice: the channel is told, instead of left waiting.
func TestEmptyTwicePostsNotice(t *testing.T) {
	fake, out := runScripted(t, "net/#empty2", "", "")
	if len(fake.seen) != 2 {
		t.Fatalf("model calls = %d, want 2 (no third try)", len(fake.seen))
	}
	if got := strings.Join(out, "\n"); got != noAnswer {
		t.Errorf("channel saw %q, want the notice", got)
	}
}

// An ordinary answer is never retried.
func TestAnswerIsNotRetried(t *testing.T) {
	fake, _ := runScripted(t, "net/#empty3", "an answer")
	if len(fake.seen) != 1 {
		t.Errorf("model calls = %d, want 1", len(fake.seen))
	}
}

type recordingLLM struct{ names [][]string }

func (r *recordingLLM) ChatCompletionStream(_ context.Context, req *llm.CompletionRequest, _ llm.EventStreamProcessor) <-chan *messages.StreamEvent {
	var n []string
	for _, t := range req.Tools {
		n = append(n, t.GetName())
	}
	r.names = append(r.names, n)
	ch := make(chan *messages.StreamEvent)
	close(ch)
	return ch
}

// Tools reach the model in one fixed order, whatever order pollytool's map hands them over in.
func TestToolOrderIsStable(t *testing.T) {
	rec := &recordingLLM{}
	c := stableToolOrder{rec}
	mk := func(names ...string) []tools.Tool {
		var out []tools.Tool
		for _, n := range names {
			out = append(out, &tools.Func{Name: n})
		}
		return out
	}
	c.ChatCompletionStream(context.Background(), &llm.CompletionRequest{Tools: mk("slap__slap", "imagegen__picture", "history__search")}, nil)
	c.ChatCompletionStream(context.Background(), &llm.CompletionRequest{Tools: mk("history__search", "slap__slap", "imagegen__picture")}, nil)
	want := "history__search,imagegen__picture,slap__slap"
	for _, got := range rec.names {
		if strings.Join(got, ",") != want {
			t.Errorf("tools sent as %v, want %s", got, want)
		}
	}
}
