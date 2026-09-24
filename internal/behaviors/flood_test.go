// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package behaviors

import (
	"testing"
	"time"

	"B4reMetal/metald/internal/core"
	mocktest "B4reMetal/metald/internal/testing"
)

func floodCtx(t *testing.T, source string, admin bool) *mocktest.MockChatContext {
	t.Helper()
	ctx := mocktest.NewMockContext()
	ctx.Source = source
	ctx.Admin = admin
	cfg := ctx.GetConfig()
	cfg.Bot.FloodMessages = 3
	cfg.Bot.FloodWindow = time.Minute
	cfg.Bot.FloodTimeout = 5 * time.Minute
	core.Flood().Reset("", source)
	core.Ignores().Remove("", source)
	return ctx
}

func TestFloodTripsAtThreshold(t *testing.T) {
	ctx := floodCtx(t, "flooder", false)
	defer core.Ignores().Remove("", "flooder")

	for i := 1; i < 3; i++ {
		if CheckFlood(ctx) {
			t.Fatalf("tripped early on message %d", i)
		}
	}
	if !CheckFlood(ctx) {
		t.Fatal("should have tripped on the third message")
	}
	if !core.Ignores().IsIgnored("", "flooder") {
		t.Fatal("flooder should have been auto-ignored")
	}
}

// An operator must never be auto-timed-out - they need to stay able to reach
// the bot, not least to undo timeouts.
func TestFloodExemptsAdmins(t *testing.T) {
	ctx := floodCtx(t, "BareMetal", true)
	defer core.Ignores().Remove("", "BareMetal")

	for i := range 10 {
		if CheckFlood(ctx) {
			t.Fatalf("admin tripped flood protection on message %d", i+1)
		}
	}
	if core.Ignores().IsIgnored("", "BareMetal") {
		t.Fatal("admin must not be auto-ignored")
	}
}

// FloodMessages = 0 turns the feature off entirely.
func TestFloodDisabled(t *testing.T) {
	ctx := floodCtx(t, "chatty", false)
	ctx.GetConfig().Bot.FloodMessages = 0
	defer core.Ignores().Remove("", "chatty")

	for range 20 {
		if CheckFlood(ctx) {
			t.Fatal("flood protection should be disabled")
		}
	}
}

// The announcement must fire once, not on every subsequent message.
func TestFloodAnnouncesOnce(t *testing.T) {
	ctx := floodCtx(t, "noisy", false)
	defer core.Ignores().Remove("", "noisy")

	for range 3 {
		CheckFlood(ctx)
	}
	if len(ctx.Replies) != 1 {
		t.Fatalf("expected exactly 1 announcement, got %d: %v", len(ctx.Replies), ctx.Replies)
	}
}
