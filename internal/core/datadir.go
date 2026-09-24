// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"path/filepath"
	"sync"
)

var (
	dataDir   = "."
	dataDirMu sync.RWMutex
)

// SetDataDir sets where runtime state files live. Call once at startup,
// before any store opens.
func SetDataDir(dir string) {
	dataDirMu.Lock()
	defer dataDirMu.Unlock()
	if dir != "" {
		dataDir = dir
	}
}

// DataPath returns the path of a state file inside the data directory.
func DataPath(name string) string {
	dataDirMu.RLock()
	defer dataDirMu.RUnlock()
	return filepath.Join(dataDir, name)
}
