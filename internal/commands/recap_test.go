// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package commands

import (
	"strings"
	"testing"

	"B4reMetal/metald/internal/core"
	mocktest "B4reMetal/metald/internal/testing"
)

func TestRecapIsAdminOnly(t *testing.T) {
	if !(&RecapCommand{}).AdminOnly() {
		t.Fatal("+recap must be admin-only: the recap can hold days of a channel's context")
	}
}

// Without a paste tool the recap is shown inline, bounded, and +recap clear deletes it.
func TestRecapShowAndClear(t *testing.T) {
	sys := mocktest.NewMockSystem()
	session, _ := sys.SessionStore.Get("net/#recapcmd")
	db, _ := core.Context()
	_ = db.SetRecap("net/#recapcmd", strings.Repeat("carol builds robots. ", 60))

	ctx := mocktest.NewMockContext().WithSystem(sys).WithSession(session).WithAdmin(true).WithArgs("+recap")
	(&RecapCommand{}).Execute(ctx)
	got := strings.Join(ctx.Replies, "\n")
	if !strings.Contains(got, "carol builds robots") || !strings.Contains(got, "more chars") {
		t.Errorf("inline recap = %q", got)
	}

	clear := mocktest.NewMockContext().WithSystem(sys).WithSession(session).WithAdmin(true).WithArgs("+recap", "clear")
	(&RecapCommand{}).Execute(clear)
	if db.Recap("net/#recapcmd") != "" {
		t.Error("recap not cleared")
	}
}

// +reset is open to everyone, so it must not take the recap with it.
func TestResetKeepsRecap(t *testing.T) {
	sys := mocktest.NewMockSystem()
	session, _ := sys.SessionStore.Get("net/#resetkeeps")
	db, _ := core.Context()
	_ = db.SetRecap("net/#resetkeeps", "days of context")

	ctx := mocktest.NewMockContext().WithSystem(sys).WithSession(session).WithArgs("+reset")
	(&ResetCommand{}).Execute(ctx)
	if db.Recap("net/#resetkeeps") != "days of context" {
		t.Error("+reset wiped the recap")
	}
}
