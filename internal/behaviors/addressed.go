// Copyright (C) 2023-2026 Alex Schlessinger and soulshack contributors
// Modified 2026 by BareMetal
// SPDX-License-Identifier: GPL-3.0-only

package behaviors

import (
	"strings"

	"github.com/lrstanley/girc"

	"B4reMetal/metald/internal/commands"
	"B4reMetal/metald/internal/core"
	"B4reMetal/metald/internal/irc"
)

// AddressedBehavior handles messages addressed to the bot
type AddressedBehavior struct {
	CmdRegistry *commands.Registry
}

func (b *AddressedBehavior) Name() string {
	return "addressed"
}

func (b *AddressedBehavior) Events() []string {
	return []string{girc.PRIVMSG}
}

func (b *AddressedBehavior) Check(ctx irc.ChatContextInterface, event *girc.Event) bool {
	if ctx.IsPrivate() && ctx.GetConfig().Bot.IgnorePrivate {
		return false
	}
	if IsIgnoredSource(ctx) {
		return false
	}
	if len(ctx.GetArgs()) == 0 {
		return false
	}

	// A registered command ("+backend", "+tools", ...) is handled even when the message doesn't
	// address the bot by name.
	if fields := strings.Fields(event.Last()); len(fields) > 0 {
		if _, isCommand := b.CmdRegistry.Get(strings.ToLower(fields[0])); isCommand {
			return true
		}
	}

	return ctx.IsAddressed() || ctx.IsPrivate()
}

func (b *AddressedBehavior) Execute(ctx irc.ChatContextInterface, event *girc.Event) {
	if CheckFlood(ctx) {
		return
	}
	// Lock-bypassing commands (e.g. +reset) exist to unstick a wedged lock,
	// so they must not queue behind whatever is holding it.
	if b.CmdRegistry.BypassesLock(ctx.GetCommand()) {
		b.CmdRegistry.Dispatch(ctx)
		return
	}
	core.WithRequestLock(ctx, ctx.GetLockKey(), "addressed", func() {
		b.CmdRegistry.Dispatch(ctx)
	}, func() {
		ctx.Reply("Request timed out waiting for previous operation to complete")
	})
}
