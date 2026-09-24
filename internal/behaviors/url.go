// Copyright (C) 2023-2026 Alex Schlessinger and soulshack contributors
// Modified 2026 by BareMetal
// SPDX-License-Identifier: GPL-3.0-only

package behaviors

import (
	"fmt"
	"regexp"

	"github.com/lrstanley/girc"

	"B4reMetal/metald/internal/core"
	"B4reMetal/metald/internal/irc"
	"B4reMetal/metald/internal/llm"
)

var urlPattern = regexp.MustCompile(`^https?://[^\s]+`)

// URLBehavior responds to messages containing URLs
type URLBehavior struct{}

func (b *URLBehavior) Name() string {
	return "url"
}

func (b *URLBehavior) Events() []string {
	return []string{girc.PRIVMSG}
}

func (b *URLBehavior) Check(ctx irc.ChatContextInterface, event *girc.Event) bool {
	cfg := ctx.GetConfig()
	if !cfg.Bot.URLWatcher {
		return false
	}
	if ctx.IsPrivate() && cfg.Bot.IgnorePrivate {
		return false
	}
	if ctx.IsAddressed() {
		return false
	}
	if urlPattern.MatchString(event.Last()) {
		ctx.GetLogger().Info("url_detected")
		return true
	}
	return false
}

func (b *URLBehavior) Execute(ctx irc.ChatContextInterface, event *girc.Event) {
	core.WithRequestLock(ctx, ctx.GetLockKey(), "url", func() {
		cfg := ctx.GetConfig()
		stripped, frames := irc.StripInjectionFrames(event.Last())
		if frames > 0 {
			ctx.GetLogger().Warn("injection_frames_stripped",
				"count", frames, "source", ctx.GetSource())
		}
		prompt := fmt.Sprintf("(nick:%s) %s", ctx.GetSource(), irc.SanitizeUserMessage(stripped))

		silent := cfg.Bot.URLWatcherSilent
		execCtx := irc.ChatContextInterface(ctx)
		if silent {
			dctx, cleanup, err := newDetachedContext(ctx)
			if err != nil {
				ctx.GetLogger().Error("url_behavior_error", "error", err)
				return
			}
			defer cleanup()
			execCtx = dctx
		}

		outch, err := llm.Complete(execCtx, prompt)
		if err != nil {
			ctx.GetLogger().Error("url_behavior_error", "error", err)
			if !silent {
				ctx.Reply(err.Error())
			}
			return
		}

		for res := range outch {
			if !silent {
				ctx.Reply(res)
			}
		}
	}, nil)
}
