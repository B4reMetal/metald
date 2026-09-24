// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportPluginLibPrependsOnce(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PYTHONPATH", "/existing")
	if got := ExportPluginLib(dir); got != dir {
		t.Fatalf("got %q, want %q", got, dir)
	}
	ExportPluginLib(dir)
	want := dir + string(os.PathListSeparator) + "/existing"
	if got := os.Getenv("PYTHONPATH"); got != want {
		t.Fatalf("PYTHONPATH = %q, want %q", got, want)
	}
}

func TestExportPluginLibMissingDirIsANoop(t *testing.T) {
	t.Setenv("PYTHONPATH", "/existing")
	if got := ExportPluginLib(filepath.Join(t.TempDir(), "nope")); got != "" {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(os.Getenv("PYTHONPATH"), "nope") {
		t.Fatal("missing dir must not be exported")
	}
}
