// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"path/filepath"
	"testing"
)

func TestDataPathDefaultsToWorkingDirectory(t *testing.T) {
	SetDataDir("")
	if got := DataPath("x.db"); got != "x.db" {
		t.Errorf("got %q", got)
	}
}

func TestDataPathJoinsDataDir(t *testing.T) {
	SetDataDir("/data")
	defer SetDataDir(".")
	if got := DataPath("x.db"); got != filepath.Join("/data", "x.db") {
		t.Errorf("got %q", got)
	}
}
