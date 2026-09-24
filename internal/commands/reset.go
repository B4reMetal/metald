// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package commands

import (
	"B4reMetal/metald/internal/core"
	"B4reMetal/metald/internal/irc"
	"fmt"
)

// ResetCommand handles +reset: wipes this channel's conversation history and makes the channel's
// request lock immediately acquirable again.
type ResetCommand struct{}

func (c *ResetCommand) Name() string    { return "+reset" }
func (c *ResetCommand) AdminOnly() bool { return false }

// restoreDefaultModel puts the model back to whatever config.yml names, undoing a "+models" switch.
func restoreDefaultModel(ctx irc.ChatContextInterface) string {
	def := DefaultModel()
	if def == "" {
		// ApplyOverrides never ran.
		return ""
	}

	cfg := ctx.GetConfig()
	if cfg.Model.Model == def {
		return ""
	}

	previous := cfg.Model.Model
	if err := configFields["model"].setter(cfg, def); err != nil {
		ctx.GetLogger().Error("model_restore_failed", "error", err.Error())
		return ""
	}
	if sys := ctx.GetSystem(); sys != nil {
		if err := sys.UpdateLLM(*cfg.API); err != nil {
			ctx.GetLogger().Error("llm_update_failed", "error", err.Error())
		}
	}
	PersistUnset("model")

	ctx.GetLogger().Info("model_restored_to_default",
		"from", previous, "to", def, "by", ctx.GetSource())
	return modelNameOnly(def)
}

// restoreOperatorPrompt undoes a "+prompt" persona, putting the operator's prompt back and re-
// enabling tools.
func restoreOperatorPrompt(ctx irc.ChatContextInterface) bool {
	if !core.Prompts().Clear(ctx.GetLockKey()) {
		return false
	}

	session := ctx.GetSession()
	metadata := session.GetMetadata()
	metadata.SystemPrompt = ctx.GetConfig().Bot.Prompt
	session.SetMetadata(metadata)

	ctx.GetLogger().Info("prompt_override_cleared",
		"channel", ctx.GetLockKey(), "by", ctx.GetSource())
	return true
}

func (c *ResetCommand) Execute(ctx irc.ChatContextInterface) {
	// Before Clear, so the rebuilt history starts from the operator's prompt.
	personaCleared := restoreOperatorPrompt(ctx)

	ctx.GetSession().Clear()

	// Back to the default model as well as a clean history, so one command
	// returns the bot to a known-good state.
	restored := restoreDefaultModel(ctx)

	cancelled := core.Requests().CancelKey(ctx.GetLockKey(), ctx.GetRequestID())

	wasHeld := core.ResetRequestLock(ctx.GetLockKey())

	ctx.GetLogger().Info("session_reset",
		"source", ctx.GetSource(),
		"lock_key", ctx.GetLockKey(),
		"lock_was_held", wasHeld,
	)

	suffix := ""
	if personaCleared {
		suffix += " Custom persona removed, tools re-enabled."
	}
	if restored != "" {
		suffix += fmt.Sprintf(" Model restored to %s.", restored)
	}

	if cancelled > 0 {
		ctx.Reply(fmt.Sprintf("Reset: conversation history cleared, %d in-flight request(s) cancelled.%s", cancelled, suffix))
		return
	}
	ctx.Reply("Reset: conversation history cleared and pending requests released." + suffix)
}
