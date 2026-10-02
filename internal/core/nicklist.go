// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import "strings"

// NickListed reports whether nick is on list, ignoring case and the trailing "_|`^" a client adds
// when its nick is taken.
func NickListed(list []string, nick string) bool {
	n := strings.ToLower(strings.TrimRight(nick, "_|`^"))
	for _, want := range list {
		if strings.ToLower(strings.TrimSpace(want)) == n {
			return true
		}
	}
	return false
}
