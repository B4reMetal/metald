// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package irc

import (
	"fmt"
	"strings"
	"unicode"
)

// CanonicalCommand maps a message's first word to the registry's "+name"
// form, or "" if it does not start with the configured prefix.
func CanonicalCommand(word, prefix string) string {
	if prefix == "" {
		prefix = "+"
	}
	if len(word) <= len(prefix) || !strings.HasPrefix(word, prefix) {
		return ""
	}
	return "+" + strings.ToLower(word[len(prefix):])
}

// ValidCommandPrefix reports why prefix cannot be used, or nil.
func ValidCommandPrefix(prefix string) error {
	if prefix == "" || len(prefix) > 8 {
		return fmt.Errorf("command prefix must be 1 to 8 characters")
	}
	for _, r := range prefix {
		if unicode.IsSpace(r) || unicode.IsLetter(r) || unicode.IsDigit(r) {
			return fmt.Errorf("command prefix must be punctuation, like + or !")
		}
	}
	return nil
}
