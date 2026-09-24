// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package commands

import (
	"fmt"
	"strings"
	"time"

	"B4reMetal/metald/internal/core"
	"B4reMetal/metald/internal/irc"
)

// defaultIgnoreDuration is used when +ignore is given a nick but no duration.
const defaultIgnoreDuration = time.Hour

// IgnoreCommand handles +ignore, for temporarily muting a nick.
type IgnoreCommand struct{}

func (c *IgnoreCommand) Name() string    { return "+ignore" }
func (c *IgnoreCommand) AdminOnly() bool { return true }

func (c *IgnoreCommand) Execute(ctx irc.ChatContextInterface) {
	args := ctx.GetArgs()

	if len(args) < 2 || args[1] == "list" {
		listIgnores(ctx)
		return
	}

	if args[1] == "remove" || args[1] == "del" {
		if len(args) < 3 {
			ctx.Reply("Usage: +ignore remove <nick>")
			return
		}
		removeIgnore(ctx, args[2])
		return
	}

	nick := args[1]
	duration := defaultIgnoreDuration
	if len(args) >= 3 {
		parsed, err := time.ParseDuration(args[2])
		if err != nil || parsed <= 0 {
			ctx.Reply(fmt.Sprintf("Invalid duration %q - use e.g. 30m, 2h, 24h", args[2]))
			return
		}
		duration = parsed
	}

	// Admins are exempt from the ignore filter, so an entry for one would silently do nothing.
	if isAdminNick(ctx, nick) {
		ctx.Reply(fmt.Sprintf("%s is an admin - admins are exempt from ignore", nick))
		return
	}
	if strings.EqualFold(nick, ctx.GetBotNick()) {
		ctx.Reply("Refusing to ignore myself")
		return
	}

	expiry := core.Ignores().Add(ctx.GetNetwork(), nick, duration)
	// An ignore that leaves their current request running would answer them
	// one more time after being told they were ignored.
	core.Requests().CancelSource(nick, ctx.GetRequestID())
	ctx.GetLogger().Info("ignore_added", "nick", nick, "duration", duration.String(), "until", expiry.UTC())
	ctx.Reply(fmt.Sprintf("Ignoring %s for %s (until %s UTC)",
		nick, duration, expiry.UTC().Format("2006-01-02 15:04")))
}

// UnignoreCommand is a convenience alias for "+ignore remove <nick>".
type UnignoreCommand struct{}

func (c *UnignoreCommand) Name() string    { return "+unignore" }
func (c *UnignoreCommand) AdminOnly() bool { return true }

func (c *UnignoreCommand) Execute(ctx irc.ChatContextInterface) {
	args := ctx.GetArgs()
	if len(args) < 2 {
		ctx.Reply("Usage: +unignore <nick>")
		return
	}
	removeIgnore(ctx, args[1])
}

func removeIgnore(ctx irc.ChatContextInterface, nick string) {
	if core.Ignores().Remove(ctx.GetNetwork(), nick) {
		ctx.GetLogger().Info("ignore_removed", "nick", nick)
		ctx.Reply(fmt.Sprintf("No longer ignoring %s", nick))
		return
	}
	ctx.Reply(fmt.Sprintf("%s wasn't ignored", nick))
}

func listIgnores(ctx irc.ChatContextInterface) {
	entries := core.Ignores().List(ctx.GetNetwork())
	if len(entries) == 0 {
		ctx.Reply("Nobody is ignored. Usage: +ignore <nick> [duration]")
		return
	}
	parts := make([]string, 0, len(entries))
	for _, e := range entries {
		remaining := time.Until(e.Expiry).Round(time.Minute)
		parts = append(parts, fmt.Sprintf("%s (%s left)", e.Nick, remaining))
	}
	ctx.Reply("Ignoring: " + strings.Join(parts, ", "))
}

// isAdminNick reports whether nick belongs to a configured admin, by
// matching the nick portion of each admin hostmask (nick!ident@host).
func isAdminNick(ctx irc.ChatContextInterface, nick string) bool {
	for _, mask := range ctx.GetConfig().Bot.Admins {
		adminNick, _, found := strings.Cut(mask, "!")
		if !found {
			adminNick = mask
		}
		if strings.EqualFold(adminNick, nick) {
			return true
		}
	}
	return false
}
