// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package llm

import (
	"strings"
	"testing"
	"time"

	"github.com/alexschlessinger/pollytool/messages"

	"B4reMetal/metald/internal/irc"
	mocktest "B4reMetal/metald/internal/testing"
)

// An empty final reply after a url-bearing tool call falls back to posting that url.
func TestOnComplete_EmptyReplyFallsBackToToolURL(t *testing.T) {
	ctx := mocktest.NewMockContext()
	output := make(chan string, 10)
	chunker := irc.NewChunker(output, 350)
	h := newCallbackHandler(ctx, chunker, ctx.GetConfig())

	// Simulate a successful generate_image call that returned a url, and no
	// content ever written (the model's final turn was empty).
	h.onToolEnd(
		messages.ChatMessageToolCall{Name: "image_gen__generate_image"},
		"url: https://i.ibb.co/example.png",
		50*time.Millisecond,
		nil,
	)
	h.onComplete(&messages.ChatMessage{})
	chunker.Flush()
	close(output)

	var got []string
	for line := range output {
		got = append(got, line)
	}

	if len(got) != 1 || got[0] != "https://i.ibb.co/example.png" {
		t.Fatalf("expected fallback url as sole output, got: %v", got)
	}
}

// The fallback applies only when the reply was empty; a normal reply gets no duplicate url.
func TestOnComplete_NoFallbackWhenContentPresent(t *testing.T) {
	ctx := mocktest.NewMockContext()
	output := make(chan string, 10)
	chunker := irc.NewChunker(output, 350)
	h := newCallbackHandler(ctx, chunker, ctx.GetConfig())

	h.onToolEnd(
		messages.ChatMessageToolCall{Name: "image_gen__generate_image"},
		"url: https://i.ibb.co/example.png",
		50*time.Millisecond,
		nil,
	)
	h.onContent("here you go: https://i.ibb.co/example.png")
	h.onComplete(&messages.ChatMessage{})
	chunker.Flush()
	close(output)

	var got []string
	for line := range output {
		got = append(got, line)
	}

	if len(got) != 1 || got[0] != "here you go: https://i.ibb.co/example.png" {
		t.Fatalf("expected only the model's own reply, no fallback appended, got: %v", got)
	}
}

// An empty reply with no url-bearing tool call before it produces no output.
func TestOnComplete_NoFallbackWithoutToolURL(t *testing.T) {
	ctx := mocktest.NewMockContext()
	output := make(chan string, 10)
	chunker := irc.NewChunker(output, 350)
	h := newCallbackHandler(ctx, chunker, ctx.GetConfig())

	h.onComplete(&messages.ChatMessage{})
	chunker.Flush()
	close(output)

	var got []string
	for line := range output {
		got = append(got, line)
	}

	if len(got) != 0 {
		t.Fatalf("expected no output, got: %v", got)
	}
}

// A retried tool call within one request (e.g. after a failed safety check) is announced once.
func TestOnToolStart_DedupesAnnouncementsWithinRequest(t *testing.T) {
	ctx := mocktest.NewMockContext()
	cfg := ctx.GetConfig()
	cfg.Bot.ShowToolActions = true
	output := make(chan string, 10)
	chunker := irc.NewChunker(output, 350)
	h := newCallbackHandler(ctx, chunker, cfg)

	calls := []messages.ChatMessageToolCall{{Name: "imagegen__picture"}}
	h.onToolStart(calls) // first attempt
	h.onToolStart(calls) // retry after a failed safety check, same request

	if len(ctx.Actions) != 1 {
		t.Fatalf("expected exactly 1 tool-call announcement, got %d: %v", len(ctx.Actions), ctx.Actions)
	}
	if ctx.Actions[0] != "calling imagegen" {
		t.Errorf("unexpected announcement text: %q", ctx.Actions[0])
	}
}

// TestOnToolStart_AnnouncesNewToolsAfterDedup makes sure a genuinely different tool called later in
// the same request still gets its own announcement - only exact repeats are suppressed.
func TestOnToolStart_AnnouncesNewToolsAfterDedup(t *testing.T) {
	ctx := mocktest.NewMockContext()
	cfg := ctx.GetConfig()
	cfg.Bot.ShowToolActions = true
	output := make(chan string, 10)
	chunker := irc.NewChunker(output, 350)
	h := newCallbackHandler(ctx, chunker, cfg)

	h.onToolStart([]messages.ChatMessageToolCall{{Name: "imagegen__picture"}})
	h.onToolStart([]messages.ChatMessageToolCall{{Name: "imagegen__picture"}})
	h.onToolStart([]messages.ChatMessageToolCall{{Name: "vision__view_image"}})

	if len(ctx.Actions) != 2 {
		t.Fatalf("expected 2 announcements (dedup then a new tool), got %d: %v", len(ctx.Actions), ctx.Actions)
	}
	if ctx.Actions[1] != "calling view_image" {
		t.Errorf("unexpected second announcement text: %q", ctx.Actions[1])
	}
}

// Announcements must read as English in a channel.
func TestToolAnnouncementReadsAsEnglish(t *testing.T) {
	cases := map[string]string{
		// overridden: the title alone would be "song" / "picture"
		"musicgen__song":    "musicgen",
		"imagegen__picture": "imagegen",
		// default path: the title IS the verb and reads correctly
		"irc__ignore":           "ignore",
		"irc__remind":           "remind",
		"memory__remember":      "remember",
		"memory__recall":        "recall",
		"sandbox__run_code":     "run_code",
		"websearch__web_search": "web_search",
		"paste__post_gist":      "post_gist",
		"vision__view_image":    "view_image",
	}
	for toolName, want := range cases {
		if got := toolDisplayName(toolName); got != want {
			t.Errorf("%s -> %q, want %q", toolName, got, want)
		}
	}
}

// A refused tool call must not leave its arguments in the session.
func TestRefusedToolArgumentsAreRedacted(t *testing.T) {
	msgs := []messages.ChatMessage{
		{Role: "assistant", ToolCalls: []messages.ChatMessageToolCall{
			{ID: "call_bad", Name: "sandbox__run_code", Arguments: `{"code":"import os\nwhile True: os.fork()"}`},
			{ID: "call_ok", Name: "memory__recall", Arguments: `{"subject":"alice"}`},
		}},
		{Role: "tool", ToolCallID: "call_bad", Content: "Refused: process spawning is not allowed"},
		{Role: "tool", ToolCallID: "call_ok", Content: "1 memory(ies) about alice"},
	}

	out := redactRefusedArguments(msgs)

	var refusedArgs, allowedArgs string
	for _, m := range out {
		for _, c := range m.ToolCalls {
			if c.ID == "call_bad" {
				refusedArgs = c.Arguments
			}
			if c.ID == "call_ok" {
				allowedArgs = c.Arguments
			}
		}
	}
	if strings.Contains(refusedArgs, "os.fork") {
		t.Errorf("refused arguments survived into the session: %q", refusedArgs)
	}
	if !strings.Contains(refusedArgs, "redacted") {
		t.Errorf("expected a redaction marker, got %q", refusedArgs)
	}
	// An unrelated call in the same turn must be untouched.
	if !strings.Contains(allowedArgs, "alice") {
		t.Errorf("an allowed call was redacted too: %q", allowedArgs)
	}
	// The refusal text itself must survive, or the model retries blindly.
	var sawRefusal bool
	for _, m := range out {
		if m.ToolCallID == "call_bad" && strings.Contains(m.GetContent(), "Refused") {
			sawRefusal = true
		}
	}
	if !sawRefusal {
		t.Error("the refusal result must be kept so the model does not retry")
	}
}

// A turn with no refusals is returned untouched.
func TestCleanTurnIsNotRewritten(t *testing.T) {
	msgs := []messages.ChatMessage{
		{Role: "assistant", ToolCalls: []messages.ChatMessageToolCall{
			{ID: "c1", Name: "memory__recall", Arguments: `{"subject":"bob"}`}}},
		{Role: "tool", ToolCallID: "c1", Content: "2 memory(ies) about bob"},
	}
	out := redactRefusedArguments(msgs)
	if out[0].ToolCalls[0].Arguments != `{"subject":"bob"}` {
		t.Errorf("clean turn was modified: %q", out[0].ToolCalls[0].Arguments)
	}
}

// The live leak, end to end through the handler: "[thinking]" opener, "</think>" closer, reasoning
// between, reply after.
func TestOnContent_StripsReasoningWrittenAsContent(t *testing.T) {
	ctx := mocktest.NewMockContext()
	output := make(chan string, 20)
	chunker := irc.NewChunker(output, 350)
	h := newCallbackHandler(ctx, chunker, ctx.GetConfig())

	for _, c := range []string{
		"[", "thinking", "]\nRight, he's saying: turn them into barstools.",
		"\n\nI should generate a picture. Keep it short.",
		"\n</think>\n\nright", ", an industrial revolution.", " give me a minute.",
	} {
		h.onContent(c)
	}
	h.flush()
	close(output)

	var got []string
	for line := range output {
		got = append(got, line)
	}
	if len(got) != 1 || got[0] != "right, an industrial revolution. give me a minute." {
		t.Fatalf("expected only the reply, got %v", got)
	}
	if !h.hadContent {
		t.Error("the visible reply counts as content")
	}
}

// An unclosed block leaves hadContent false, so the tool-url fallback in
// onComplete still fires instead of the channel getting silence.
func TestOnContent_UnclosedReasoningIsNotContent(t *testing.T) {
	ctx := mocktest.NewMockContext()
	output := make(chan string, 20)
	chunker := irc.NewChunker(output, 350)
	h := newCallbackHandler(ctx, chunker, ctx.GetConfig())

	h.onContent("<think>\nthinking about it")
	h.onContent(" and more thinking")
	h.flush()
	close(output)

	for line := range output {
		t.Errorf("unclosed reasoning reached output: %q", line)
	}
	if h.hadContent {
		t.Error("suppressed reasoning must not count as content")
	}
}

// The live failure the reasoning filter could not catch: the model deliberated in first person with
// NO think markers at all and posted fifteen consecutive lines to the channel.
func TestOnContent_BudgetLatchesRunawayTurn(t *testing.T) {
	ctx := mocktest.NewMockContext()
	output := make(chan string, 200)
	chunker := irc.NewChunker(output, 350)
	h := newCallbackHandler(ctx, chunker, ctx.GetConfig())

	line := strings.Repeat("deliberating about the checker, ", 8) + "\n"
	for i := 0; i < 15; i++ {
		h.onContent(line)
	}
	h.flush()
	close(output)

	var total int
	for l := range output {
		total += len(l)
	}
	if total > maxTurnContent+len(line) {
		t.Errorf("runaway turn posted %d bytes, budget is %d", total, maxTurnContent)
	}
	if !h.overBudget {
		t.Error("the budget should have latched")
	}
}

// An ordinary reply must be nowhere near the budget and must arrive whole.
func TestOnContent_OrdinaryReplyUnaffected(t *testing.T) {
	ctx := mocktest.NewMockContext()
	output := make(chan string, 20)
	chunker := irc.NewChunker(output, 350)
	h := newCallbackHandler(ctx, chunker, ctx.GetConfig())

	reply := "of course it's my true form, what did you think, a server rack with opinions."
	h.onContent(reply)
	h.flush()
	close(output)

	var got []string
	for l := range output {
		got = append(got, l)
	}
	if len(got) != 1 || got[0] != reply {
		t.Errorf("ordinary reply was altered: %v", got)
	}
	if h.overBudget {
		t.Error("an ordinary reply must not trip the budget")
	}
}

// A turn cut off by the budget still surrenders the link it generated - the
// flood is the problem, not the song.
func TestOnComplete_BudgetedTurnStillPostsToolURL(t *testing.T) {
	ctx := mocktest.NewMockContext()
	output := make(chan string, 200)
	chunker := irc.NewChunker(output, 350)
	h := newCallbackHandler(ctx, chunker, ctx.GetConfig())

	h.onToolEnd(messages.ChatMessageToolCall{Name: "musicgen__song"},
		"url: https://files.example.com/u/abc.flac", time.Millisecond, nil)
	h.onContent(strings.Repeat("rambling on and on and on. ", 200))
	h.onComplete(&messages.ChatMessage{})
	h.flush()
	close(output)

	var found bool
	for l := range output {
		if strings.Contains(l, "https://files.example.com/u/abc.flac") {
			found = true
		}
	}
	if !found {
		t.Error("the generated link was lost when the budget latched")
	}
}

// discard() throws away what a dead request was still holding.
func TestDiscardDropsHeldOutput(t *testing.T) {
	ctx := mocktest.NewMockContext()
	output := make(chan string, 20)
	chunker := irc.NewChunker(output, 350)
	h := newCallbackHandler(ctx, chunker, ctx.GetConfig())

	h.onContent("a sentence that stops halfway")
	if n := h.discard(); n == 0 {
		t.Error("expected buffered bytes to be discarded")
	}
	h.flush()
	close(output)

	for l := range output {
		t.Errorf("a dead request emitted %q", l)
	}
}

// drainQueued empties the backlog a dead request left in the output channel.
func TestDrainQueuedEmptiesBacklog(t *testing.T) {
	ch := make(chan string, 10)
	for i := 0; i < 7; i++ {
		ch <- "queued ramble"
	}
	if n := drainQueued(ch); n != 7 {
		t.Errorf("drained %d, want 7", n)
	}
	if len(ch) != 0 {
		t.Errorf("backlog survived: %d lines", len(ch))
	}
	if n := drainQueued(ch); n != 0 {
		t.Errorf("draining an empty channel returned %d", n)
	}
}
