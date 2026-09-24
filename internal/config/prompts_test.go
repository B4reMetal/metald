// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func fullBot() *BotConfig {
	return &BotConfig{
		FloorPrompt: "a", GatekeeperPreamble: "b", GatekeeperPolicy: "c",
		ClassifyPreamble: "d", ReplyScreenPolicy: "e", MemoryPolicy: "f", MemoryFrame: "g",
	}
}

func TestMissingPromptsNoneWhenAllSet(t *testing.T) {
	if m := MissingPrompts(fullBot()); len(m) != 0 {
		t.Errorf("expected nothing missing, got %v", m)
	}
}

// Whitespace is not a prompt. A key set to "" or "  \n" in yaml must be
// reported, or the classifier would run with an empty policy and screen
// nothing while appearing configured.
func TestMissingPromptsReportsBlankAndWhitespace(t *testing.T) {
	b := fullBot()
	b.GatekeeperPolicy = ""
	b.MemoryPolicy = " \n\t"
	got := MissingPrompts(b)
	want := []string{"gatekeeperpolicy", "memorypolicy"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestMissingPromptsNilReportsAll(t *testing.T) {
	if got := MissingPrompts(nil); len(got) != len(PromptKeys) {
		t.Errorf("nil config should report every key, got %v", got)
	}
}

// The shipped example config is the only source of default prompt text, so
// it must actually carry all of them, non-empty, or a fresh install cannot
// start.
func TestShippedExampleConfigHasEveryPrompt(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "examples", "chatbot.yml"))
	if err != nil {
		t.Fatalf("read example config: %v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse example config: %v", err)
	}
	for _, k := range PromptKeys {
		v, ok := doc[k].(string)
		if !ok || isBlank(v) {
			t.Errorf("examples/chatbot.yml is missing prompt %q", k)
		}
	}
}
