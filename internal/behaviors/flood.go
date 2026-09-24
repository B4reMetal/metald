// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package behaviors

import (
	"fmt"

	"B4reMetal/metald/internal/core"
	"B4reMetal/metald/internal/irc"
)

// CheckFlood records this message against the sender's recent rate and, if they've crossed the
// configured threshold, auto-ignores them for a while.
func CheckFlood(ctx irc.ChatContextInterface) bool {
	cfg := ctx.GetConfig()
	if cfg.Bot.FloodMessages <= 0 {
		return false
	}

	// Admins are exempt, same as the manual and model-driven ignore paths - an operator must always be
	// able to reach the bot, especially to undo an auto-timeout.
	if ctx.IsAdmin() {
		return false
	}

	source := ctx.GetSource()
	if source == "" {
		return false
	}

	count := core.Flood().Record(ctx.GetNetwork(), source, cfg.Bot.FloodWindow)
	if count < cfg.Bot.FloodMessages {
		return false
	}

	// Say why, or the person just sees the bot go silent.
	ctx.Reply(fmt.Sprintf("%s: too many messages too fast. ignoring you for %s.",
		source, cfg.Bot.FloodTimeout))

	expiry := core.Ignores().Add(ctx.GetNetwork(), source, cfg.Bot.FloodTimeout)
	// Clear the history so they resume from a clean slate when the timeout
	// lapses, instead of instantly re-tripping on their first message back.
	core.Flood().Reset(ctx.GetNetwork(), source)

	// Stop anything this nick already has running, excluding THIS request -
	// which is one of theirs, and is the one doing the announcing.
	if n := core.Requests().CancelSource(source, ctx.GetRequestID()); n > 0 {
		ctx.GetLogger().Info("flood_cancelled_inflight", "source", source, "requests", n)
	}

	ctx.GetLogger().Warn("flood_timeout",
		"source", source,
		"messages", count,
		"window", cfg.Bot.FloodWindow.String(),
		"timeout", cfg.Bot.FloodTimeout.String(),
		"until", expiry.UTC(),
	)

	return true
}
