// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package config

// PromptKeys are the model-facing prompts that must be set in config. There
// are no built-in defaults: the shipped text lives in examples/chatbot.yml,
// and the bot refuses to start with any of these empty.
var PromptKeys = []string{
	"floorprompt",
	"gatekeeperpreamble",
	"gatekeeperpolicy",
	"classifypreamble",
	"replyscreenpolicy",
	"memorypolicy",
	"memoryframe",
}

// MissingPrompts returns the prompt keys that are empty or whitespace.
func MissingPrompts(b *BotConfig) []string {
	if b == nil {
		return PromptKeys
	}
	vals := map[string]string{
		"floorprompt":        b.FloorPrompt,
		"gatekeeperpreamble": b.GatekeeperPreamble,
		"gatekeeperpolicy":   b.GatekeeperPolicy,
		"classifypreamble":   b.ClassifyPreamble,
		"replyscreenpolicy":  b.ReplyScreenPolicy,
		"memorypolicy":       b.MemoryPolicy,
		"memoryframe":        b.MemoryFrame,
	}
	var missing []string
	for _, k := range PromptKeys {
		if isBlank(vals[k]) {
			missing = append(missing, k)
		}
	}
	return missing
}

func isBlank(s string) bool {
	for _, r := range s {
		if r != ' ' && r != '\n' && r != '\t' && r != '\r' {
			return false
		}
	}
	return true
}
