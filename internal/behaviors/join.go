// Copyright (C) 2023-2026 Alex Schlessinger and soulshack contributors
// Modified 2026 by BareMetal
// SPDX-License-Identifier: GPL-3.0-only

package behaviors

import (
	"github.com/lrstanley/girc"

	"B4reMetal/metald/internal/core"
	"B4reMetal/metald/internal/irc"
	"B4reMetal/metald/internal/llm"
)

// JoinBehavior sends a greeting when the bot joins a channel.
type JoinBehavior struct{}

func (b *JoinBehavior) Name() string {
	return "join"
}

func (b *JoinBehavior) Events() []string {
	return []string{girc.JOIN}
}

func (b *JoinBehavior) Check(ctx irc.ChatContextInterface, event *girc.Event) bool {
	cfg := ctx.GetConfig()
	return event.Source.Name == ctx.GetBotNick() && cfg.Bot.Greeting != ""
}

func (b *JoinBehavior) Execute(ctx irc.ChatContextInterface, event *girc.Event) {
	core.WithRequestLock(ctx, ctx.GetLockKey(), "join", func() {
		cfg := ctx.GetConfig()
		outch, err := llm.Complete(ctx, cfg.Bot.Greeting)
		if err != nil {
			ctx.GetLogger().Error("join_behavior_error", "error", err)
			ctx.Reply(err.Error())
			return
		}

		for res := range outch {
			ctx.Reply(res)
		}
	}, nil)
}
