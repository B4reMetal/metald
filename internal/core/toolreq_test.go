// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"os"
	"path/filepath"
	"testing"
)

func fakeTool(t *testing.T, schema string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "tool.sh")
	body := "#!/bin/sh\n[ \"$1\" = --schema ] && printf '%s' '" + schema + "'\n"
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestShellToolRequirementsReadsList(t *testing.T) {
	p := fakeTool(t, `{"title":"x","requires":["A_KEY","B_URL"]}`)
	got, err := ShellToolRequirements(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "A_KEY" || got[1] != "B_URL" {
		t.Errorf("got %v", got)
	}
}

func TestShellToolRequirementsAbsentMeansNone(t *testing.T) {
	p := fakeTool(t, `{"title":"x","sandbox":{"allowNetwork":true}}`)
	got, err := ShellToolRequirements(p)
	if err != nil || len(got) != 0 {
		t.Errorf("got %v, %v", got, err)
	}
}

func TestShellToolRequirementsBadOutputIsAnError(t *testing.T) {
	p := fakeTool(t, `not json`)
	if _, err := ShellToolRequirements(p); err == nil {
		t.Error("expected an error")
	}
}

func TestMissingEnvTreatsBlankAsMissing(t *testing.T) {
	env := map[string]string{"SET": "x", "BLANK": "  "}
	got := MissingEnv([]string{"SET", "BLANK", "UNSET"}, func(k string) string { return env[k] })
	if len(got) != 2 || got[0] != "BLANK" || got[1] != "UNSET" {
		t.Errorf("got %v", got)
	}
}
