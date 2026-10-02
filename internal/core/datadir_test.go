// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"path/filepath"
	"testing"
)

// keepDataDir restores the data directory TestMain chose once a test that changes it is done.
func keepDataDir(t *testing.T) {
	prev := DataPath("")
	t.Cleanup(func() { SetDataDir(prev) })
}

func TestDataPathDefaultsToWorkingDirectory(t *testing.T) {
	keepDataDir(t)
	SetDataDir(".")
	SetDataDir("")
	if got := DataPath("x.db"); got != "x.db" {
		t.Errorf("got %q", got)
	}
}

func TestDataPathJoinsDataDir(t *testing.T) {
	keepDataDir(t)
	SetDataDir("/data")
	if got := DataPath("x.db"); got != filepath.Join("/data", "x.db") {
		t.Errorf("got %q", got)
	}
}
