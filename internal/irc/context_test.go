// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package irc

import (
	"context"
	"testing"
	"time"
)

// A cancelled request must go completely quiet.
func TestSilencedOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := ChatContext{Context: ctx}

	reason, quiet := c.silenced()
	if !quiet {
		t.Fatal("a cancelled request must not be allowed to write to the channel")
	}
	if reason != "request_cancelled" {
		t.Errorf("expected the cancellation reason, got %q", reason)
	}
}

// A deadline expiry is NOT cancellation.
func TestNotSilencedOnDeadlineExceeded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()

	if ctx.Err() != context.DeadlineExceeded {
		t.Fatalf("test setup: expected DeadlineExceeded, got %v", ctx.Err())
	}

	c := ChatContext{Context: ctx}

	if reason, quiet := c.silenced(); quiet {
		t.Errorf("a timed-out request must still report the failure, got silenced as %q", reason)
	}
}

// The ordinary path: a live request talks.
func TestNotSilencedWhenHealthy(t *testing.T) {
	c := ChatContext{Context: context.Background()}

	if reason, quiet := c.silenced(); quiet {
		t.Errorf("a healthy request must be able to reply, got silenced as %q", reason)
	}
}

// A model that writes its tool call out as prose must not reach the channel.
func TestLeakedToolCallDetection(t *testing.T) {
	block := []string{
		"<tool_call>",
		"</tool_call>",
		"\n\n<tool_call>\n<function=music_gen__\n<parameter=lyrics>\n[Verse]\nlate night in the lab",
		"<function=music_gen__generate_music>",
		"<function =image_gen__generate_image>",
		"<parameter=lyrics>",
		"<invoke name=\"sandbox\">",
		`{"tool_calls": [{"name":"x"}]}`,
		"<function_call>",
	}
	for _, m := range block {
		if !leakedToolCall.MatchString(m) {
			t.Errorf("should be suppressed: %q", m)
		}
	}

	// Closing tags matter as much as opening ones.
	for _, m := range []string{"</function>", "</parameter>", "</invoke>", "<function=image_gen__prompt>"} {
		if !leakedToolCall.MatchString(m) {
			t.Errorf("closing/opening tag must be suppressed: %q", m)
		}
	}

	allow := []string{
		"that function is broken",
		"the tool call failed, try again",
		"write a function that reverses a string",
		"[Verse] the server room is cold tonight",
		"check the parameter you passed",
		"i invoke the ancient rite of rebooting it",
		"your function= assignment is a syntax error", // prose, not a tag
		"see <https://example.com/function=1>",
	}
	for _, m := range allow {
		if leakedToolCall.MatchString(m) {
			t.Errorf("false positive on ordinary text: %q", m)
		}
	}
}
