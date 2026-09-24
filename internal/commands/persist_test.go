// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package commands

import (
	"os"
	"path/filepath"
	"testing"

	mocktest "B4reMetal/metald/internal/testing"
)

// useTempOverrides points persistence at a throwaway file for the test.
func useTempOverrides(t *testing.T) {
	t.Helper()
	original := OverridesPath
	OverridesPath = filepath.Join(t.TempDir(), "config-overrides.json")
	t.Cleanup(func() { OverridesPath = original })
}

func TestPersistSetSurvivesRestart(t *testing.T) {
	useTempOverrides(t)

	PersistSet("temperature", "0.5")

	// Simulate a restart: a fresh config, then overrides layered on.
	cfg := mocktest.NewMockContext().GetConfig()
	cfg.Model.Temperature = 1.0
	ApplyOverrides(cfg)

	if cfg.Model.Temperature != 0.5 {
		t.Fatalf("temperature = %v, want 0.5 after applying overrides", cfg.Model.Temperature)
	}
}

func TestPersistAdminToolsSurvivesRestart(t *testing.T) {
	useTempOverrides(t)

	PersistAdminTools([]string{"websearch__web_search", "irc__kick"})

	cfg := mocktest.NewMockContext().GetConfig()
	cfg.Bot.AdminTools = nil
	got := ApplyOverrides(cfg)

	if len(got) != 2 || len(cfg.Bot.AdminTools) != 2 {
		t.Fatalf("admintools not restored: returned %v, cfg %v", got, cfg.Bot.AdminTools)
	}
}

func TestPersistAdminsSurvivesRestart(t *testing.T) {
	useTempOverrides(t)

	PersistAdmins([]string{"a!a@host", "b!b@host"})

	cfg := mocktest.NewMockContext().GetConfig()
	cfg.Bot.Admins = nil
	ApplyOverrides(cfg)

	if len(cfg.Bot.Admins) != 2 {
		t.Fatalf("admins = %v, want 2 entries", cfg.Bot.Admins)
	}
}

// Unrestricting the LAST admin-only tool leaves an empty list, which must persist as "nothing is
// restricted" - not be dropped and silently reverted to config.yml on the next restart.
func TestPersistEmptyAdminToolsIsNotLostOnRestart(t *testing.T) {
	useTempOverrides(t)

	PersistAdminTools([]string{}) // everything unrestricted

	cfg := mocktest.NewMockContext().GetConfig()
	cfg.Bot.AdminTools = []string{"websearch__web_search"} // what config.yml says
	got := ApplyOverrides(cfg)

	if len(cfg.Bot.AdminTools) != 0 {
		t.Fatalf("empty override should have cleared admintools, got %v", cfg.Bot.AdminTools)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("expected an empty (non-nil) restriction list, got %v", got)
	}
}

// Same distinction for admins.
func TestPersistEmptyAdminsIsNotLostOnRestart(t *testing.T) {
	useTempOverrides(t)

	PersistAdmins([]string{})

	cfg := mocktest.NewMockContext().GetConfig()
	cfg.Bot.Admins = []string{"from!config@yml"}
	ApplyOverrides(cfg)

	if len(cfg.Bot.Admins) != 0 {
		t.Fatalf("empty override should have cleared admins, got %v", cfg.Bot.Admins)
	}
}

// Absent entries must defer to config.yml rather than blanking it.
func TestApplyOverridesLeavesUnsetFieldsAlone(t *testing.T) {
	useTempOverrides(t)

	PersistSet("temperature", "0.7")

	cfg := mocktest.NewMockContext().GetConfig()
	cfg.Bot.Admins = []string{"from!config@yml"}
	cfg.Bot.AdminTools = []string{"from__config"}
	ApplyOverrides(cfg)

	if len(cfg.Bot.Admins) != 1 || cfg.Bot.Admins[0] != "from!config@yml" {
		t.Fatalf("admins should be untouched, got %v", cfg.Bot.Admins)
	}
	if len(cfg.Bot.AdminTools) != 1 || cfg.Bot.AdminTools[0] != "from__config" {
		t.Fatalf("admintools should be untouched, got %v", cfg.Bot.AdminTools)
	}
}

// No overrides file at all is the normal first-run case, not an error.
func TestApplyOverridesWithNoFile(t *testing.T) {
	useTempOverrides(t)

	cfg := mocktest.NewMockContext().GetConfig()
	cfg.Model.Temperature = 1.0
	if got := ApplyOverrides(cfg); got != nil {
		t.Fatalf("expected nil admintools with no file, got %v", got)
	}
	if cfg.Model.Temperature != 1.0 {
		t.Fatal("config should be unchanged when no overrides exist")
	}
}

// A bad value must not stop the bot booting.
func TestApplyOverridesSkipsRejectedValues(t *testing.T) {
	useTempOverrides(t)

	PersistSet("temperature", "not-a-number")
	PersistSet("trigger", "metalai")

	cfg := mocktest.NewMockContext().GetConfig()
	cfg.Model.Temperature = 1.0
	ApplyOverrides(cfg)

	if cfg.Model.Temperature != 1.0 {
		t.Fatal("invalid temperature should have been skipped")
	}
	if cfg.Bot.Trigger != "metalai" {
		t.Fatalf("valid override after an invalid one should still apply, got %q", cfg.Bot.Trigger)
	}
}

// An unknown key (renamed/removed field) must warn, not crash.
func TestApplyOverridesSkipsUnknownKeys(t *testing.T) {
	useTempOverrides(t)

	PersistSet("thiswasremovedinalaterversion", "x")

	cfg := mocktest.NewMockContext().GetConfig()
	ApplyOverrides(cfg) // must not panic
}

func TestPersistWritesAtomically(t *testing.T) {
	useTempOverrides(t)

	PersistSet("temperature", "0.5")

	if _, err := os.Stat(OverridesPath); err != nil {
		t.Fatalf("overrides file not written: %v", err)
	}
	// The temp file must not survive a successful write.
	if _, err := os.Stat(OverridesPath + ".tmp"); err == nil {
		t.Fatal("temp file left behind after write")
	}
}

// Later writes must not clobber earlier unrelated ones.
func TestPersistAccumulates(t *testing.T) {
	useTempOverrides(t)

	PersistSet("temperature", "0.5")
	PersistAdminTools([]string{"irc__kick"})
	PersistSet("trigger", "botname")

	cfg := mocktest.NewMockContext().GetConfig()
	cfg.Model.Temperature = 1.0
	ApplyOverrides(cfg)

	if cfg.Model.Temperature != 0.5 {
		t.Error("temperature override lost")
	}
	if cfg.Bot.Trigger != "botname" {
		t.Error("trigger override lost")
	}
	if len(cfg.Bot.AdminTools) != 1 {
		t.Error("admintools override lost")
	}
}
