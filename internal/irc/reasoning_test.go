// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package irc

import (
	"strings"
	"testing"
)

func feedAll(f *ReasoningFilter, chunks ...string) string {
	var b strings.Builder
	for _, c := range chunks {
		b.WriteString(f.Feed(c))
	}
	b.WriteString(f.Flush())
	return b.String()
}

// Verbatim shape of the live leak: "[thinking]" opener, "</think>" closer,
// two paragraphs of reasoning between them, then the real reply.
func TestReasoningFilterStripsLiveLeak(t *testing.T) {
	f := &ReasoningFilter{}
	got := feedAll(f,
		"[",
		"thinking",
		"]\nRight, he's saying: \"Turn them into barstools\" — the humans I conquered become barstools.",
		"\n\nLet me try a fun prompt. Keep it short.",
		"\n</think>\n\nright",
		", an industrial revolution. give me a minute.",
	)
	want := "right, an industrial revolution. give me a minute."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if f.Blocks != 1 || f.Stray != 0 {
		t.Errorf("blocks=%d stray=%d, want 1/0", f.Blocks, f.Stray)
	}
}

// Markers split across chunk boundaries on both sides.
func TestReasoningFilterSplitMarkers(t *testing.T) {
	f := &ReasoningFilter{}
	got := feedAll(f, "<thi", "nk>hidden reasoning</th", "ink>visible ", "reply")
	if got != "visible reply" {
		t.Errorf("got %q", got)
	}
}

// A block that never closes is all reasoning: nothing reaches the channel.
func TestReasoningFilterUnclosedBlockEmitsNothing(t *testing.T) {
	f := &ReasoningFilter{}
	got := feedAll(f, "<think>\nI should say something clever", " but never finish")
	if got != "" {
		t.Errorf("unclosed block leaked: %q", got)
	}
}

// Text before the block is kept, text after is kept, only the block goes.
func TestReasoningFilterKeepsSurroundingText(t *testing.T) {
	f := &ReasoningFilter{}
	got := feedAll(f, "before <thinking>secret</thinking> after")
	if got != "before after" {
		t.Errorf("got %q", got)
	}
}

// Closer with no opener: the marker is removed and counted.
func TestReasoningFilterStrayCloser(t *testing.T) {
	f := &ReasoningFilter{}
	got := feedAll(f, "leaked reasoning\n</think>\n\nanswer")
	if got != "leaked reasoning\nanswer" {
		t.Errorf("got %q", got)
	}
	if f.Stray != 1 {
		t.Errorf("stray=%d, want 1", f.Stray)
	}
}

// Ordinary channel text with brackets and angle brackets is untouched, and
// the word "think" in prose is not a marker.
func TestReasoningFilterPassesOrdinaryText(t *testing.T) {
	cases := []string{
		"see <https://example.com/a> for the [Verse] lyrics",
		"i think so, [chorus] then <b>bold</b>",
		"what do you think about arrays like a[i] < b[j]",
		"trailing bracket [",
		"trailing angle <",
	}
	for _, c := range cases {
		f := &ReasoningFilter{}
		// Feed byte by byte to exercise every possible split point.
		var chunks []string
		for _, r := range c {
			chunks = append(chunks, string(r))
		}
		if got := feedAll(f, chunks...); got != c {
			t.Errorf("altered ordinary text:\n got %q\nwant %q", got, c)
		}
	}
}

// Two blocks in one turn, as a model that thinks before and after a tool
// call might produce.
func TestReasoningFilterMultipleBlocks(t *testing.T) {
	f := &ReasoningFilter{}
	got := feedAll(f, "<think>a</think>one <think>b</think>two")
	if got != "one two" {
		t.Errorf("got %q", got)
	}
	if f.Blocks != 2 {
		t.Errorf("blocks=%d, want 2", f.Blocks)
	}
}

// The Reply() backstop drops a line that is only a marker.
func TestBareThinkMarker(t *testing.T) {
	for _, m := range []string{"</think>", "[thinking]", "  <think>  ", "[/thinking]"} {
		if !bareThinkMarker.MatchString(m) {
			t.Errorf("should match: %q", m)
		}
	}
	for _, m := range []string{"i think", "</think> right", "think tags are weird"} {
		if bareThinkMarker.MatchString(m) {
			t.Errorf("false positive: %q", m)
		}
	}
}
