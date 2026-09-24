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

// PromptCommand handles +prompt: lets anyone replace the bot's persona for this channel, at the
// cost of every tool.
type PromptCommand struct{}

func (c *PromptCommand) Name() string    { return "+prompt" }
func (c *PromptCommand) AdminOnly() bool { return false }

// maxUserPrompt bounds what one message can install.
const maxUserPrompt = 2000

// effectivePrompt is what actually gets installed as the system prompt.
func effectivePrompt(floor bool, floorText, text string) string {
	if !floor {
		return text
	}
	return floorText + text
}

func (c *PromptCommand) Execute(ctx irc.ChatContextInterface) {
	key := ctx.GetLockKey()

	// No argument: report the current state rather than clearing it. Clearing
	// is +reset's job, so that "how do I undo this" has exactly one answer.
	if len(ctx.GetArgs()) < 2 {
		if o, ok := core.Prompts().Get(key); ok {
			preview := o.Prompt
			if len(preview) > 120 {
				preview = preview[:120] + "..."
			}
			ctx.Reply(fmt.Sprintf("custom persona active (set by %s, tools disabled): %s  -- use +reset to restore", o.Source, preview))
			return
		}
		ctx.Reply("no custom persona; running the operator's prompt with tools enabled. \"+prompt <text>\" sets one, which disables every tool until +reset.")
		return
	}

	text := strings.TrimSpace(strings.Join(ctx.GetArgs()[1:], " "))
	if text == "" {
		ctx.Reply("give me a persona to run: +prompt <text>")
		return
	}
	if len(text) > maxUserPrompt {
		ctx.Reply(fmt.Sprintf("too long - %d characters, limit is %d", len(text), maxUserPrompt))
		return
	}

	session := ctx.GetSession()
	metadata := session.GetMetadata()
	metadata.SystemPrompt = effectivePrompt(ctx.GetConfig().Bot.PromptFloor, ctx.GetConfig().Bot.FloorPrompt, text)
	session.SetMetadata(metadata)

	session.Clear()

	core.Prompts().Set(key, text, ctx.GetSource())

	// Logged in full: this is a user rewriting what the bot is, and the exact
	// text is the evidence if the result needs explaining later.
	ctx.GetLogger().Info("prompt_override_set",
		"channel", key, "source", ctx.GetSource(), "length", len(text), "prompt", text)

	ctx.Reply("prompt set")
}
