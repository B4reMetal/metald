// Copyright (C) 2023-2026 Alex Schlessinger and soulshack contributors
// Modified 2026 by BareMetal
// SPDX-License-Identifier: GPL-3.0-only

package behaviors

import (
	"github.com/lrstanley/girc"

	"B4reMetal/metald/internal/commands"
	"B4reMetal/metald/internal/core"
	"B4reMetal/metald/internal/irc"
)

// NonAddressedBehavior handles all messages when addressed mode is disabled
type NonAddressedBehavior struct {
	CmdRegistry *commands.Registry
}

func (b *NonAddressedBehavior) Name() string {
	return "nonaddressed"
}

func (b *NonAddressedBehavior) Events() []string {
	return []string{girc.PRIVMSG}
}

func (b *NonAddressedBehavior) Check(ctx irc.ChatContextInterface, event *girc.Event) bool {
	cfg := ctx.GetConfig()
	if IsIgnoredSource(ctx) {
		return false
	}
	return !cfg.Bot.Addressed && !ctx.IsAddressed() && !ctx.IsPrivate() && len(ctx.GetArgs()) > 0
}

func (b *NonAddressedBehavior) Execute(ctx irc.ChatContextInterface, event *girc.Event) {
	if CheckFlood(ctx) {
		return
	}
	// See AddressedBehavior.Execute - lock-bypassing commands must not queue
	// behind the request they exist to clear.
	if b.CmdRegistry.BypassesLock(ctx.GetCommand()) {
		b.CmdRegistry.Dispatch(ctx)
		return
	}
	core.WithRequestLock(ctx, ctx.GetLockKey(), "nonaddressed", func() {
		b.CmdRegistry.Dispatch(ctx)
	}, func() {
		ctx.Reply("Request timed out waiting for previous operation to complete")
	})
}
