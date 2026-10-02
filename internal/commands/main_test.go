// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package commands

import (
	"B4reMetal/metald/internal/core"
	"os"
	"testing"
)

// TestMain keeps every store the tests open in a throwaway directory.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "metald-test-")
	if err != nil {
		panic(err)
	}
	core.SetDataDir(dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
