// Copyright (C) 2023-2026 Alex Schlessinger and soulshack contributors
// Modified 2026 by BareMetal
// SPDX-License-Identifier: GPL-3.0-only

package commands

import (
	"B4reMetal/metald/internal/irc"
)

// VersionCommand handles the +version command
type VersionCommand struct {
	Version string
}

func (c *VersionCommand) Name() string    { return "+version" }
func (c *VersionCommand) AdminOnly() bool { return false }

func (c *VersionCommand) Execute(ctx irc.ChatContextInterface) {
	ctx.Reply(c.Version)
}
