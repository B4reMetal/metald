// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package behaviors

import (
	"B4reMetal/metald/internal/core"
	"B4reMetal/metald/internal/irc"
)

// IsIgnoredSource reports whether the message's sender is currently on the temporary ignore list,
// and should therefore get no response at all.
func IsIgnoredSource(ctx irc.ChatContextInterface) bool {
	source := ctx.GetSource()
	if source == "" {
		return false
	}
	if !core.Ignores().IsIgnored(ctx.GetNetwork(), source) {
		return false
	}
	if ctx.IsAdmin() {
		return false
	}
	ctx.GetLogger().Debug("message_ignored", "source", source)
	return true
}
