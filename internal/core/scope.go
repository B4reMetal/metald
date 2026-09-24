// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import "strings"

// ScopeKey namespaces a per-network key.
func ScopeKey(network, key string) string {
	if network == "" {
		return key
	}
	return network + "/" + key
}

// UnscopeKey strips a network prefix, for displaying a key back to humans.
// Returns the key unchanged when it carries no prefix.
func UnscopeKey(network, key string) string {
	if network == "" {
		return key
	}
	return strings.TrimPrefix(key, network+"/")
}

// keyInNetwork reports whether a scoped key belongs to a network, used when
// listing or sweeping one network's entries out of a shared map.
func keyInNetwork(network, key string) bool {
	if network == "" {
		// The unscoped path: a key with no separator belongs to the only
		// network there is.
		return !strings.Contains(key, "/")
	}
	return strings.HasPrefix(key, network+"/")
}
