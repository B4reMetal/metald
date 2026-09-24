// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ShellToolRequirements asks a shell tool for its schema and returns the
// "requires" list: environment variables it cannot work without.
func ShellToolRequirements(command string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, command, "--schema").Output()
	if err != nil {
		return nil, err
	}
	var meta struct {
		Requires []string `json:"requires"`
	}
	if err := json.Unmarshal(out, &meta); err != nil {
		return nil, err
	}
	return meta.Requires, nil
}

// MissingEnv returns the keys that are unset or blank.
func MissingEnv(keys []string, getenv func(string) string) []string {
	if getenv == nil {
		getenv = os.Getenv
	}
	var missing []string
	for _, k := range keys {
		if strings.TrimSpace(getenv(k)) == "" {
			missing = append(missing, k)
		}
	}
	return missing
}
