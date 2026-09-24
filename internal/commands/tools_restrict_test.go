// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package commands

import (
	"strings"
	"testing"

	"github.com/alexschlessinger/pollytool/schema"
	"github.com/alexschlessinger/pollytool/tools"

	"B4reMetal/metald/internal/irc"
	mocktest "B4reMetal/metald/internal/testing"
)

func stubTool(name string) tools.Tool {
	return &tools.Func{
		Name:   name,
		Desc:   "stub",
		Params: schema.Params{},
	}
}

// toolsCtx builds a context with the given tools registered and the caller
// as an admin (restrict/unrestrict are admin-gated).
func toolsCtx(t *testing.T, names ...string) *mocktest.MockChatContext {
	t.Helper()
	ctx := mocktest.NewMockContext().WithSystem(mocktest.NewMockSystem())
	ctx.Admin = true
	reg := ctx.GetSystem().GetToolRegistry()
	for _, n := range names {
		reg.Register(stubTool(n))
	}
	return ctx
}

func lastReply(t *testing.T, ctx *mocktest.MockChatContext) string {
	t.Helper()
	if len(ctx.Replies) == 0 {
		t.Fatal("expected a reply, got none")
	}
	return ctx.Replies[len(ctx.Replies)-1]
}

func TestRestrictSingleTool(t *testing.T) {
	ctx := toolsCtx(t, "websearch__web_search")
	ctx.Args = []string{"+tools", "restrict", "websearch__web_search"}

	(&ToolsCommand{}).Execute(ctx)

	tool, ok := ctx.GetSystem().GetToolRegistry().Get("websearch__web_search")
	if !ok {
		t.Fatal("tool vanished from the registry")
	}
	if !irc.IsAdminOnly(tool) {
		t.Fatal("tool should now be admin-only")
	}
	if !strings.Contains(lastReply(t, ctx), "Restricted to admins") {
		t.Fatalf("unexpected reply: %s", lastReply(t, ctx))
	}
}

// Lifting a restriction has to put the ORIGINAL tool back, not a wrapper -
// otherwise the admin check would still fire.
func TestUnrestrictRestoresOpenAccess(t *testing.T) {
	ctx := toolsCtx(t, "websearch__web_search")

	ctx.Args = []string{"+tools", "restrict", "websearch__web_search"}
	(&ToolsCommand{}).Execute(ctx)

	ctx.Args = []string{"+tools", "unrestrict", "websearch__web_search"}
	(&ToolsCommand{}).Execute(ctx)

	tool, _ := ctx.GetSystem().GetToolRegistry().Get("websearch__web_search")
	if irc.IsAdminOnly(tool) {
		t.Fatal("tool should be unrestricted again")
	}
	if !strings.Contains(lastReply(t, ctx), "Available to everyone") {
		t.Fatalf("unexpected reply: %s", lastReply(t, ctx))
	}
}

// A bare namespace applies to every tool under it.
func TestRestrictWholeNamespace(t *testing.T) {
	ctx := toolsCtx(t, "irc__op", "irc__kick", "vision__view_image")
	ctx.Args = []string{"+tools", "restrict", "irc"}

	(&ToolsCommand{}).Execute(ctx)

	reg := ctx.GetSystem().GetToolRegistry()
	for _, n := range []string{"irc__op", "irc__kick"} {
		tool, _ := reg.Get(n)
		if !irc.IsAdminOnly(tool) {
			t.Errorf("%s should be admin-only", n)
		}
	}
	// Must not spill into an unrelated namespace.
	other, _ := reg.Get("vision__view_image")
	if irc.IsAdminOnly(other) {
		t.Error("vision__view_image should be untouched")
	}
}

func TestRestrictNoMatch(t *testing.T) {
	ctx := toolsCtx(t, "irc__op")
	ctx.Args = []string{"+tools", "restrict", "nosuchtool__nope"}

	(&ToolsCommand{}).Execute(ctx)
	if !strings.Contains(lastReply(t, ctx), "No tools matched") {
		t.Fatalf("unexpected reply: %s", lastReply(t, ctx))
	}
}

// Re-restricting an already-restricted tool must not double-wrap it.
func TestRestrictIsIdempotent(t *testing.T) {
	ctx := toolsCtx(t, "irc__op")

	ctx.Args = []string{"+tools", "restrict", "irc__op"}
	(&ToolsCommand{}).Execute(ctx)
	(&ToolsCommand{}).Execute(ctx)

	if !strings.Contains(lastReply(t, ctx), "No change") {
		t.Fatalf("second restrict should report no change, got: %s", lastReply(t, ctx))
	}
	tool, _ := ctx.GetSystem().GetToolRegistry().Get("irc__op")
	if irc.IsAdminOnly(irc.UnwrapAdminOnly(tool)) {
		t.Fatal("tool was double-wrapped")
	}
}

// "+get admintools" should reflect runtime changes.
func TestRestrictSyncsConfig(t *testing.T) {
	ctx := toolsCtx(t, "websearch__web_search")

	ctx.Args = []string{"+tools", "restrict", "websearch__web_search"}
	(&ToolsCommand{}).Execute(ctx)
	if !containsStr(ctx.GetConfig().Bot.AdminTools, "websearch__web_search") {
		t.Fatal("admintools config should list the restricted tool")
	}

	ctx.Args = []string{"+tools", "unrestrict", "websearch__web_search"}
	(&ToolsCommand{}).Execute(ctx)
	if containsStr(ctx.GetConfig().Bot.AdminTools, "websearch__web_search") {
		t.Fatal("admintools config should no longer list the tool")
	}
}

// Non-admins must not be able to change permissions.
func TestRestrictRequiresAdmin(t *testing.T) {
	ctx := toolsCtx(t, "irc__op")
	ctx.Admin = false
	ctx.Args = []string{"+tools", "restrict", "irc__op"}

	(&ToolsCommand{}).Execute(ctx)

	if !strings.Contains(lastReply(t, ctx), "permission") {
		t.Fatalf("expected a permission denial, got: %s", lastReply(t, ctx))
	}
	tool, _ := ctx.GetSystem().GetToolRegistry().Get("irc__op")
	if irc.IsAdminOnly(tool) {
		t.Fatal("a non-admin changed tool permissions")
	}
}

func containsStr(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
