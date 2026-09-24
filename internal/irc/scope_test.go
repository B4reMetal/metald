// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package irc

import "testing"

// Two networks that both have a "#general" must not share a key.
func TestScopeKeySeparatesNetworks(t *testing.T) {
	live := scopeKey("examplenet", "#general")
	test := scopeKey("testbed", "#general")

	if live == test {
		t.Fatalf("two networks collided on key %q", live)
	}
	if live != "examplenet/#general" {
		t.Errorf("unexpected key format: %q", live)
	}
}

// The single-network path must be byte-identical to what it was before multi-network existed, or
// every stored session is orphaned on upgrade and every channel silently starts with no history.
func TestScopeKeyUnchangedForSingleNetwork(t *testing.T) {
	for _, key := range []string{"#chat", "someuser", "#a/b"} {
		if got := scopeKey("", key); got != key {
			t.Errorf("single-network key changed: %q -> %q", key, got)
		}
	}
}

// Per-nick keys (private messages) are scoped too: the same person on two
// networks is two conversations.
func TestScopeKeySeparatesPrivateMessages(t *testing.T) {
	if scopeKey("a", "greg") == scopeKey("b", "greg") {
		t.Error("the same nick on two networks shared a key")
	}
}
