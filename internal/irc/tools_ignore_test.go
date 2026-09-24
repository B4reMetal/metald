// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package irc

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alexschlessinger/pollytool/tools"

	"B4reMetal/metald/internal/core"
	mocktest "B4reMetal/metald/internal/testing"
)

func runIgnoreTool(t *testing.T, ctx context.Context, args tools.Args) string {
	t.Helper()
	out, err := newIrcIgnoreTool().Execute(ctx, args)
	if err != nil {
		t.Fatalf("tool returned error: %v", err)
	}
	return out
}

// The bot may mute someone for at most an hour on its own initiative, no
// matter what the model asks for.
func TestSelfIgnoreCapsAtOneHour(t *testing.T) {
	mock := mocktest.NewMockContext()
	ctx := InjectContext(context.Background(), mock)

	core.Ignores().Remove("", "floodbot")
	defer core.Ignores().Remove("", "floodbot")

	out := runIgnoreTool(t, ctx, tools.Args{
		"nick": "floodbot", "minutes": 6000, "reason": "flooding",
	})
	if !strings.Contains(out, "60 minute") {
		t.Fatalf("expected the request to be capped to 60 minutes, got: %s", out)
	}

	for _, e := range core.Ignores().List("") {
		if e.Nick == "floodbot" && time.Until(e.Expiry) > maxSelfIgnoreDuration+time.Minute {
			t.Fatalf("expiry exceeded the cap: %s", time.Until(e.Expiry))
		}
	}
}

// A zero/negative duration must not create a permanent or nonsensical entry.
func TestSelfIgnoreFloorsAtOneMinute(t *testing.T) {
	mock := mocktest.NewMockContext()
	ctx := InjectContext(context.Background(), mock)

	core.Ignores().Remove("", "zerodur")
	defer core.Ignores().Remove("", "zerodur")

	out := runIgnoreTool(t, ctx, tools.Args{
		"nick": "zerodur", "minutes": 0, "reason": "test",
	})
	if !strings.Contains(out, "1 minute") {
		t.Fatalf("expected a 1 minute floor, got: %s", out)
	}
}

// The bot must never be able to silence an operator - they need to stay able
// to reach it to undo the ignore.
func TestSelfIgnoreRefusesAdmin(t *testing.T) {
	mock := mocktest.NewMockContext()
	mock.GetConfig().Bot.Admins = []string{"BareMetal!BareMetal@irc.examplenet.org"}
	ctx := InjectContext(context.Background(), mock)

	defer core.Ignores().Remove("", "BareMetal")

	out := runIgnoreTool(t, ctx, tools.Args{
		"nick": "BareMetal", "minutes": 30, "reason": "annoying",
	})
	if !strings.Contains(out, "cannot be ignored") {
		t.Fatalf("expected refusal for an admin, got: %s", out)
	}
	if core.Ignores().IsIgnored("", "BareMetal") {
		t.Fatal("admin must not have been added to the ignore store")
	}
}

// Admin matching is on the nick portion of the hostmask, case-insensitively.
func TestSelfIgnoreRefusesAdminDifferentCase(t *testing.T) {
	mock := mocktest.NewMockContext()
	mock.GetConfig().Bot.Admins = []string{"BareMetal!BareMetal@irc.examplenet.org"}
	ctx := InjectContext(context.Background(), mock)

	defer core.Ignores().Remove("", "baremetal")

	out := runIgnoreTool(t, ctx, tools.Args{
		"nick": "baremetal", "minutes": 30, "reason": "annoying",
	})
	if !strings.Contains(out, "cannot be ignored") {
		t.Fatalf("expected case-insensitive admin refusal, got: %s", out)
	}
}

func TestSelfIgnoreRefusesSelf(t *testing.T) {
	mock := mocktest.NewMockContext()
	ctx := InjectContext(context.Background(), mock)

	out := runIgnoreTool(t, ctx, tools.Args{
		"nick": mock.GetBotNick(), "minutes": 5, "reason": "oops",
	})
	if !strings.Contains(out, "Refusing to ignore myself") {
		t.Fatalf("expected self-ignore refusal, got: %s", out)
	}
}

func TestSelfIgnoreActuallyIgnores(t *testing.T) {
	mock := mocktest.NewMockContext()
	ctx := InjectContext(context.Background(), mock)

	core.Ignores().Remove("", "spammer")
	defer core.Ignores().Remove("", "spammer")

	runIgnoreTool(t, ctx, tools.Args{
		"nick": "spammer", "minutes": 15, "reason": "flooding",
	})
	if !core.Ignores().IsIgnored("", "spammer") {
		t.Fatal("nick should be ignored after the tool runs")
	}
}
