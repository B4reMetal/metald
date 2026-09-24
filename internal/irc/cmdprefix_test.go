// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package irc

import "testing"

func TestCanonicalCommand(t *testing.T) {
	for _, c := range []struct{ word, prefix, want string }{
		{"+help", "+", "+help"},
		{"+HELP", "+", "+help"},
		{"!help", "!", "+help"},
		{"!!reset", "!!", "+reset"},
		{"+help", "!", ""},
		{"help", "!", ""},
		{"!", "!", ""},
		{"+ignore", "", "+ignore"},
	} {
		if got := CanonicalCommand(c.word, c.prefix); got != c.want {
			t.Errorf("CanonicalCommand(%q, %q) = %q, want %q", c.word, c.prefix, got, c.want)
		}
	}
}

func TestValidCommandPrefix(t *testing.T) {
	for _, ok := range []string{"+", "!", "!!", ".", "~>"} {
		if err := ValidCommandPrefix(ok); err != nil {
			t.Errorf("%q should be valid: %v", ok, err)
		}
	}
	for _, bad := range []string{"", " ", "a", "bot:", "!9", "123456789"} {
		if ValidCommandPrefix(bad) == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}
