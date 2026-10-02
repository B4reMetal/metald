// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package commands

import (
	"fmt"
	"strings"

	"B4reMetal/metald/internal/core"
	"B4reMetal/metald/internal/irc"
	"B4reMetal/metald/internal/llm"
)

// recapInline bounds a recap shown in the channel when there is no paste tool.
const recapInline = 600

// RecapCommand shows or clears this channel's recap: the running summary of its older conversation.
type RecapCommand struct{}

func (c *RecapCommand) Name() string    { return "+recap" }
func (c *RecapCommand) AdminOnly() bool { return true }

func (c *RecapCommand) Execute(ctx irc.ChatContextInterface) {
	db, err := core.Context()
	if err != nil {
		ctx.GetLogger().Error("context_db_unavailable", "error", err.Error())
		ctx.Reply("the recap is unavailable")
		return
	}
	key := ctx.GetSession().GetName()
	args := ctx.GetArgs()

	if len(args) > 1 {
		if strings.ToLower(args[1]) != "clear" {
			ctx.Reply("usage: +recap | +recap clear")
			return
		}
		if db.ClearRecap(key) {
			ctx.GetLogger().Info("recap_cleared", "key", key, "by", ctx.GetSource())
			ctx.Reply("recap cleared.")
		} else {
			ctx.Reply("there is no recap to clear.")
		}
		return
	}

	recap := db.Recap(key)
	if recap == "" {
		ctx.Reply("no recap yet: nothing in this channel has been folded.")
		return
	}
	if url := pasteRecap(ctx, recap); url != "" {
		ctx.Reply(fmt.Sprintf("recap (%d chars): %s", len(recap), url))
		return
	}
	shown := recap
	if len(shown) > recapInline {
		shown = shown[:recapInline] + fmt.Sprintf("... (%d more chars)", len(recap)-recapInline)
	}
	for _, line := range strings.Split(shown, "\n") {
		if strings.TrimSpace(line) != "" {
			ctx.Reply(line)
		}
	}
}

// pasteRecap posts the recap with the paste tool and returns its url, or "" if that is not possible.
func pasteRecap(ctx irc.ChatContextInterface, recap string) string {
	return llm.PasteText(ctx.GetSystem(), recap, ctx.GetLogger())
}
