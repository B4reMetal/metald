// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package commands

import (
	"strings"
	"testing"

	mocktest "B4reMetal/metald/internal/testing"
)

func prefixRegistry() *Registry {
	r := NewRegistry()
	r.Register(&IgnoreCommand{})
	r.Register(&UnignoreCommand{})
	r.Register(NewHelpCommand(r))
	return r
}

func TestRepliesUseConfiguredPrefix(t *testing.T) {
	r := prefixRegistry()
	ctx := mocktest.NewMockContext().WithAdmin(true).WithArgs("!ignore", "remove")
	ctx.Command = "+ignore"
	ctx.GetConfig().Bot.CommandPrefix = "!"
	r.Dispatch(ctx)
	got := lastReply(t, ctx)
	if !strings.Contains(got, "!ignore remove") || strings.Contains(got, "+ignore") {
		t.Fatalf("reply not rewritten: %q", got)
	}
}

func TestHelpListsConfiguredPrefix(t *testing.T) {
	r := prefixRegistry()
	ctx := mocktest.NewMockContext().WithAdmin(true).WithArgs("!help")
	ctx.Command = "+help"
	ctx.GetConfig().Bot.CommandPrefix = "!"
	r.Dispatch(ctx)
	got := lastReply(t, ctx)
	if !strings.Contains(got, "!help") || !strings.Contains(got, "!unignore") || strings.Contains(got, "+help") {
		t.Fatalf("help not prefixed: %q", got)
	}
}

func TestRewriteLeavesOtherPlusWordsAlone(t *testing.T) {
	r := prefixRegistry()
	ctx := mocktest.NewMockContext()
	ctx.GetConfig().Bot.CommandPrefix = "!"
	p := r.withPrefix(ctx).(prefixedReplies)
	in := "try +ignore now; 2+ignore stays, +ignored stays, C++ stays, a +o mode stays"
	want := "try !ignore now; 2+ignore stays, +ignored stays, C++ stays, a +o mode stays"
	if got := p.rewrite(in); got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestDefaultPrefixIsUntouched(t *testing.T) {
	r := prefixRegistry()
	ctx := mocktest.NewMockContext()
	if _, wrapped := r.withPrefix(ctx).(prefixedReplies); wrapped {
		t.Fatal("the default + prefix must not wrap the context")
	}
}
