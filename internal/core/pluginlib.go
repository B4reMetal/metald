// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"os"
	"path/filepath"
	"strings"
)

// ExportPluginLib prepends dir to PYTHONPATH so every tool the bot runs,
// shipped or custom, can import the shared metald_tools package. Returns
// the absolute path exported, or "" if dir does not exist.
func ExportPluginLib(dir string) string {
	if dir == "" {
		return ""
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		return ""
	}
	parts := []string{abs}
	for _, p := range filepath.SplitList(os.Getenv("PYTHONPATH")) {
		if p != "" && p != abs {
			parts = append(parts, p)
		}
	}
	os.Setenv("PYTHONPATH", strings.Join(parts, string(os.PathListSeparator)))
	return abs
}
