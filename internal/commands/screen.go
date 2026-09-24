// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package commands

import (
	"fmt"
	"strings"

	"B4reMetal/metald/internal/core"
	"B4reMetal/metald/internal/irc"
)

// ScreenCommand handles +screen: put a nick behind the inbound gate and the
// outbound reply check. Edits screennicks/filternicks in the live config and
// persists them with the other runtime overrides.
type ScreenCommand struct{}

func (c *ScreenCommand) Name() string    { return "+screen" }
func (c *ScreenCommand) AdminOnly() bool { return true }

func (c *ScreenCommand) Execute(ctx irc.ChatContextInterface) {
	args := ctx.GetArgs()
	if len(args) < 2 || args[1] == "list" {
		listScreens(ctx)
		return
	}
	if args[1] == "remove" || args[1] == "del" {
		if len(args) < 3 {
			ctx.Reply("Usage: +screen remove <nick>")
			return
		}
		removeScreen(ctx, args[2])
		return
	}

	nick := args[1]
	if isAdminNick(ctx, nick) {
		ctx.Reply(fmt.Sprintf("%s is an admin - not screening", nick))
		return
	}
	if strings.EqualFold(nick, ctx.GetBotNick()) {
		ctx.Reply("Refusing to screen myself")
		return
	}
	bot := ctx.GetConfig().Bot
	addedIn := addNick(&bot.ScreenNicks, nick)
	addedOut := addNick(&bot.FilterNicks, nick)
	if !addedIn && !addedOut {
		ctx.Reply(fmt.Sprintf("%s is already screened", nick))
		return
	}
	PersistScreening(bot.ScreenNicks, bot.FilterNicks)
	// Their earlier turns in this conversation were never screened; drop
	// them so nothing already planted keeps steering the model.
	dropped := 0
	if session := ctx.GetSession(); session != nil {
		dropped = core.QuarantineSpeaker(session, nick)
	}
	ctx.GetLogger().Info("screen_added", "nick", nick, "quarantined", dropped)
	ctx.Reply(fmt.Sprintf("Screening %s: messages gated, replies checked, %d earlier turns dropped", nick, dropped))
}

// UnscreenCommand is an alias for "+screen remove <nick>".
type UnscreenCommand struct{}

func (c *UnscreenCommand) Name() string    { return "+unscreen" }
func (c *UnscreenCommand) AdminOnly() bool { return true }

func (c *UnscreenCommand) Execute(ctx irc.ChatContextInterface) {
	args := ctx.GetArgs()
	if len(args) < 2 {
		ctx.Reply("Usage: +unscreen <nick>")
		return
	}
	removeScreen(ctx, args[1])
}

func removeScreen(ctx irc.ChatContextInterface, nick string) {
	bot := ctx.GetConfig().Bot
	removedIn := removeNick(&bot.ScreenNicks, nick)
	removedOut := removeNick(&bot.FilterNicks, nick)
	if !removedIn && !removedOut {
		ctx.Reply(fmt.Sprintf("%s wasn't screened", nick))
		return
	}
	PersistScreening(bot.ScreenNicks, bot.FilterNicks)
	ctx.GetLogger().Info("screen_removed", "nick", nick)
	ctx.Reply(fmt.Sprintf("No longer screening %s", nick))
}

func listScreens(ctx irc.ChatContextInterface) {
	bot := ctx.GetConfig().Bot
	if len(bot.ScreenNicks) == 0 && len(bot.FilterNicks) == 0 {
		ctx.Reply("Nobody is screened. Usage: +screen <nick> | +screen remove <nick>")
		return
	}
	ctx.Reply(fmt.Sprintf("Screening - inbound: %s; outbound: %s",
		joinOrNone(bot.ScreenNicks), joinOrNone(bot.FilterNicks)))
}

func joinOrNone(list []string) string {
	if len(list) == 0 {
		return "(none)"
	}
	return strings.Join(list, ", ")
}

// addNick appends nick unless it is already listed (case-insensitive).
func addNick(list *[]string, nick string) bool {
	for _, have := range *list {
		if strings.EqualFold(strings.TrimSpace(have), nick) {
			return false
		}
	}
	*list = append(*list, nick)
	return true
}

func removeNick(list *[]string, nick string) bool {
	kept := (*list)[:0:0]
	removed := false
	for _, have := range *list {
		if strings.EqualFold(strings.TrimSpace(have), nick) {
			removed = true
			continue
		}
		kept = append(kept, have)
	}
	*list = kept
	return removed
}
