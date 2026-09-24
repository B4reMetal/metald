// Copyright (C) 2023-2026 Alex Schlessinger and soulshack contributors
// Modified 2026 by BareMetal
// SPDX-License-Identifier: GPL-3.0-only

package commands

import (
	"fmt"
	"strings"

	"B4reMetal/metald/internal/core"

	"B4reMetal/metald/internal/irc"
	"B4reMetal/metald/internal/llm"
)

// CompletionCommand handles the default chat completion
type CompletionCommand struct{}

func (c *CompletionCommand) Name() string    { return "" }
func (c *CompletionCommand) AdminOnly() bool { return false }

func (c *CompletionCommand) Execute(ctx irc.ChatContextInterface) {
	msg := strings.Join(ctx.GetArgs(), " ")

	cleaned, frames := irc.StripInjectionFrames(msg)
	if frames > 0 {
		score := core.Suspicions().Add(ctx.GetNetwork(), ctx.GetSource(), core.SignalInjectionFrames)
		ctx.GetLogger().Warn("injection_frames_stripped",
			"count", frames, "source", ctx.GetSource(), "suspicion", score)
	}

	outch, err := llm.Complete(ctx, fmt.Sprintf("(nick:%s) %s", ctx.GetSource(), irc.SanitizeUserMessage(cleaned)))

	if err != nil {
		ctx.GetLogger().Error("completion_error", "error", err)
		ctx.Reply(err.Error())
		return
	}

	for res := range outch {
		ctx.Reply(res)
	}
}
