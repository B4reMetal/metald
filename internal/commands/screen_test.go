// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package commands

import (
	"path/filepath"
	"strings"
	"testing"

	"B4reMetal/metald/internal/config"
	mocktest "B4reMetal/metald/internal/testing"
)

func screenCtx(t *testing.T, args ...string) *mocktest.MockChatContext {
	t.Helper()
	original := OverridesPath
	OverridesPath = filepath.Join(t.TempDir(), "config-overrides.json")
	t.Cleanup(func() { OverridesPath = original })
	return mocktest.NewMockContext().WithAdmin(true).WithArgs(args...)
}

func TestScreenCommandIsAdminOnly(t *testing.T) {
	if !(&ScreenCommand{}).AdminOnly() || !(&UnscreenCommand{}).AdminOnly() {
		t.Fatal("screen commands must be admin-only")
	}
}

func TestScreenAddListRemoveEditsConfigAndPersists(t *testing.T) {
	ctx := screenCtx(t, "+screen", "mallory")
	(&ScreenCommand{}).Execute(ctx)
	if !strings.HasPrefix(lastReply(t, ctx), "Screening mallory") {
		t.Fatalf("add reply: %q", lastReply(t, ctx))
	}
	bot := ctx.GetConfig().Bot
	if len(bot.ScreenNicks) != 1 || len(bot.FilterNicks) != 1 {
		t.Fatalf("config not edited: in=%v out=%v", bot.ScreenNicks, bot.FilterNicks)
	}

	fresh := &config.Configuration{Bot: &config.BotConfig{}, Model: &config.ModelConfig{}}
	ApplyOverrides(fresh)
	if len(fresh.Bot.ScreenNicks) != 1 || fresh.Bot.ScreenNicks[0] != "mallory" || len(fresh.Bot.FilterNicks) != 1 {
		t.Fatalf("not persisted across restart: in=%v out=%v", fresh.Bot.ScreenNicks, fresh.Bot.FilterNicks)
	}

	ctx.WithArgs("+screen", "list")
	(&ScreenCommand{}).Execute(ctx)
	if !strings.Contains(lastReply(t, ctx), "inbound: mallory; outbound: mallory") {
		t.Fatalf("list reply: %q", lastReply(t, ctx))
	}

	ctx.WithArgs("+unscreen", "Mallory")
	(&UnscreenCommand{}).Execute(ctx)
	if lastReply(t, ctx) != "No longer screening Mallory" || len(bot.ScreenNicks) != 0 || len(bot.FilterNicks) != 0 {
		t.Fatalf("remove: %q in=%v out=%v", lastReply(t, ctx), bot.ScreenNicks, bot.FilterNicks)
	}
	fresh = &config.Configuration{Bot: &config.BotConfig{ScreenNicks: []string{"mallory"}}, Model: &config.ModelConfig{}}
	ApplyOverrides(fresh)
	if len(fresh.Bot.ScreenNicks) != 0 {
		t.Fatal("removal not persisted: config.yml entry should be superseded by the empty override")
	}
}

func TestUnscreenRemovesConfigListedNick(t *testing.T) {
	ctx := screenCtx(t, "+unscreen", "eve")
	ctx.GetConfig().Bot.ScreenNicks = []string{"eve"}
	ctx.GetConfig().Bot.FilterNicks = []string{"eve", "bob"}
	(&UnscreenCommand{}).Execute(ctx)
	if lastReply(t, ctx) != "No longer screening eve" {
		t.Fatalf("got %q", lastReply(t, ctx))
	}
	if got := ctx.GetConfig().Bot.FilterNicks; len(got) != 1 || got[0] != "bob" {
		t.Fatalf("filternicks = %v", got)
	}
}

func TestScreenRefusesSelfAndAdmins(t *testing.T) {
	ctx := screenCtx(t, "+screen", "x")
	ctx.WithArgs("+screen", ctx.GetBotNick())
	(&ScreenCommand{}).Execute(ctx)
	if lastReply(t, ctx) != "Refusing to screen myself" {
		t.Fatalf("self: %q", lastReply(t, ctx))
	}
	ctx.WithArgs("+screen", "alice")
	ctx.GetConfig().Bot.Admins = []string{"alice!*@*"}
	(&ScreenCommand{}).Execute(ctx)
	if !strings.Contains(lastReply(t, ctx), "is an admin") {
		t.Fatalf("admin: %q", lastReply(t, ctx))
	}
}
