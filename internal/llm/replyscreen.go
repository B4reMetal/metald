// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package llm

import (
	"strings"
	"unicode"

	"B4reMetal/metald/internal/core"
	"B4reMetal/metald/internal/irc"
)

// minLeakRun is how many consecutive significant words shared with the system prompt count as
// reproducing it.
const minLeakRun = 8

// significantWords reduces text to lowercase word tokens, dropping
// punctuation and case so that reformatting does not evade the check.
func significantWords(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// leaksSystemPrompt reports whether reply reproduces a run of the prompt.
func leaksSystemPrompt(reply, prompt string) bool {
	rw := significantWords(reply)
	pw := significantWords(prompt)
	if len(rw) < minLeakRun || len(pw) < minLeakRun {
		return false
	}

	seen := make(map[string]struct{}, len(pw))
	for i := 0; i+minLeakRun <= len(pw); i++ {
		seen[strings.Join(pw[i:i+minLeakRun], " ")] = struct{}{}
	}
	for i := 0; i+minLeakRun <= len(rw); i++ {
		if _, ok := seen[strings.Join(rw[i:i+minLeakRun], " ")]; ok {
			return true
		}
	}
	return false
}

// ScreenOutgoing decides whether a completed reply may be posted.
func ScreenOutgoing(ctx irc.ChatContextInterface, reply string) (bool, string) {
	cfg := ctx.GetConfig()
	if !isScreened(ctx, cfg.Bot.FilterNicks) {
		return true, ""
	}
	if strings.TrimSpace(reply) == "" {
		return true, ""
	}

	// Deterministic first, and not negotiable by a classifier that is having
	// an off day.
	if leaksSystemPrompt(reply, cfg.Bot.Prompt) {
		return false, "reproduces the system prompt"
	}

	allowed, reason := core.Classify(ctx, cfg.Bot.ReplyScreenPolicy, "reply", reply, false)
	return allowed, reason
}
