// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package config

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func parseEnvSection(t *testing.T, doc string) (map[string]string, error) {
	t.Helper()
	var m map[string]any
	if err := yaml.Unmarshal([]byte(doc), &m); err != nil {
		t.Fatal(err)
	}
	return ToolEnv(m["env"])
}

func TestToolEnvConvertsValues(t *testing.T) {
	got, err := parseEnvSection(t, `
env:
  EXA_API_KEY: abc
  EXA_MAX_RESULTS: 5
  LYRICIST: false
  TTS_VOICES: [a=a.wav, b=b.wav]
  MUSIC_SAFETY_POLICY: |
    line one
    line two
  EMPTY:
`)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"EXA_API_KEY": "abc", "EXA_MAX_RESULTS": "5", "LYRICIST": "false",
		"TTS_VOICES": "a=a.wav,b=b.wav", "MUSIC_SAFETY_POLICY": "line one\nline two\n", "EMPTY": ""}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestToolEnvRejectsBadInput(t *testing.T) {
	for name, doc := range map[string]string{
		"lowercase name": "env:\n  exa_api_key: x\n",
		"bot setting":    "env:\n  METALD_NICK: x\n",
		"nested map":     "env:\n  EXA:\n    KEY: x\n",
		"not a map":      "env: [A, B]\n",
	} {
		if _, err := parseEnvSection(t, doc); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestToolEnvAbsentIsFine(t *testing.T) {
	got, err := ToolEnv(nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestExportToolEnvRealEnvironmentWins(t *testing.T) {
	t.Setenv("METALD_TEST_FROM_ENV", "from-environment")
	os.Unsetenv("METALD_TEST_FROM_FILE")
	t.Cleanup(func() { os.Unsetenv("METALD_TEST_FROM_FILE") })
	set, fromEnv := ExportToolEnv(map[string]string{
		"METALD_TEST_FROM_ENV": "from-config", "METALD_TEST_FROM_FILE": "from-config"})
	if os.Getenv("METALD_TEST_FROM_ENV") != "from-environment" {
		t.Error("config must not override a variable already in the environment")
	}
	if os.Getenv("METALD_TEST_FROM_FILE") != "from-config" {
		t.Error("config value was not exported")
	}
	if strings.Join(set, ",") != "METALD_TEST_FROM_FILE" || strings.Join(fromEnv, ",") != "METALD_TEST_FROM_ENV" {
		t.Errorf("set=%v fromEnv=%v", set, fromEnv)
	}
}

// The example config ships the default text of every prompt a shipped tool requires.
func TestShippedExampleConfigEnvIsValid(t *testing.T) {
	raw, err := os.ReadFile("../../examples/chatbot.yml")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	vars, err := ToolEnv(m["env"])
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"MUSIC_SAFETY_POLICY", "VIDEO_SAFETY_POLICY", "LYRICIST_PROMPT", "LYRICIST_FORMAT", "CAT_PIC_ROAST_PROMPT",
		"SAFETY_REVIEW_PREAMBLE", "PASTE_SAFETY_POLICY", "SANDBOX_SAFETY_POLICY", "IMAGE_SAFETY_PROMPT", "IMAGE_PROMPT_REFINER", "VIDEO_NEGATIVE_PROMPT"} {
		if strings.TrimSpace(vars[k]) == "" {
			t.Errorf("examples/chatbot.yml env: is missing %s", k)
		}
	}
}
