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
	"sync"
	"time"
)

// ShellToolRequirements asks a shell tool for its schema and returns the
// "requires" list: environment variables it cannot work without.
// ShellToolMeta is what the bot reads from a plugin's --schema beyond the
// tool definition itself.
type ShellToolMeta struct {
	Requires []string `json:"requires"`
	// Announce false keeps "calling <tool>" out of the channel, for tools whose
	// output is itself the visible effect.
	Announce *bool `json:"announce"`
	// Requester true has the bot pass the asking nick as "requested_by".
	Requester bool `json:"requester"`
}

// ReadShellToolMeta runs a plugin's --schema and returns its metadata.
func ReadShellToolMeta(command string) (ShellToolMeta, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var meta ShellToolMeta
	out, err := exec.CommandContext(ctx, command, "--schema").Output()
	if err != nil {
		return meta, err
	}
	err = json.Unmarshal(out, &meta)
	return meta, err
}

var quietTools sync.Map

// SetQuietTool records that a tool's calls are not announced in the channel.
func SetQuietTool(name string) { quietTools.Store(name, true) }

// QuietTool reports whether a tool's calls are not announced in the channel.
func QuietTool(name string) bool {
	_, ok := quietTools.Load(name)
	return ok
}

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
