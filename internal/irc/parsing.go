// Copyright (C) 2023-2026 Alex Schlessinger and soulshack contributors
// Modified 2026 by BareMetal
// SPDX-License-Identifier: GPL-3.0-only

package irc

import (
	"errors"
	"net"
	"regexp"
	"strings"
	"unicode"
)

// isTriggerWordChar reports whether r is part of a word, so a trigger matches whole words
// only, never inside a longer word.
func isTriggerWordChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

// StripLeadingTrigger removes trigger from the very start of message, if it appears there as a
// bounded whole word/phrase, and returns the remaining whitespace-tokenized words.
func StripLeadingTrigger(message, trigger string) ([]string, bool) {
	fields := strings.Fields(message)
	trigWords := strings.Fields(trigger)
	if len(trigWords) == 0 || len(fields) < len(trigWords) {
		return nil, false
	}

	for i, w := range trigWords[:len(trigWords)-1] {
		if !strings.EqualFold(fields[i], w) {
			return nil, false
		}
	}

	last := fields[len(trigWords)-1]
	wantLast := trigWords[len(trigWords)-1]
	switch {
	case strings.EqualFold(last, wantLast):
		// exact match: trigger word is its own token
	case len(last) == len(wantLast)+1 && strings.EqualFold(last[:len(wantLast)], wantLast):
		// exactly one trailing separator glued on, e.g. "bot:" or "bot,"
		sep := last[len(wantLast)]
		if sep != ':' && sep != ',' {
			return nil, false
		}
	default:
		return nil, false
	}

	return fields[len(trigWords):], true
}

// CheckAddressed reports whether trigger appears anywhere in message as a whole word or
// phrase, case-insensitively.
func CheckAddressed(message, trigger string) bool {
	// If trigger is empty, it matches everything (legacy behavior)
	if trigger == "" {
		return true
	}

	msg := []rune(message)
	trig := []rune(trigger)
	if len(trig) == 0 || len(trig) > len(msg) {
		return false
	}

	for i := 0; i+len(trig) <= len(msg); i++ {
		match := true
		for j, r := range trig {
			if unicode.ToLower(msg[i+j]) != unicode.ToLower(r) {
				match = false
				break
			}
		}
		if !match {
			continue
		}

		beforeOK := i == 0 || !isTriggerWordChar(msg[i-1])
		after := i + len(trig)
		afterOK := after == len(msg) || !isTriggerWordChar(msg[after])
		if beforeOK && afterOK {
			return true
		}
	}
	return false
}

// CheckAdmin returns true if hostmask matches any admin in the list.
// An empty list means nobody is admin, never everybody.
func CheckAdmin(hostmask string, adminList []string) bool {
	if len(adminList) == 0 {
		return false
	}
	for _, admin := range adminList {
		if admin == hostmask {
			return true
		}
	}
	return false
}

// CheckPrivate returns true if target is not a channel (doesn't start with #).
func CheckPrivate(target string) bool {
	return !strings.HasPrefix(target, "#")
}

// RFC 2812 compliant patterns
var (
	// Nick: starts with letter or special, followed by letters, digits, special, or hyphen
	// Special chars: [\]^_`{|}
	nickRegex = regexp.MustCompile(`^[a-zA-Z\[\]\\^\x60_{|}][a-zA-Z0-9\[\]\\^\x60_{|}\-]*$`)

	// User: alphanumeric with optional ~ prefix, allows -_.
	userRegex = regexp.MustCompile(`^~?[a-zA-Z0-9_.\-]+$`)

	// Hostname: DNS labels (letters, digits, hyphens) separated by dots
	hostnameRegex = regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9\-]*[a-zA-Z0-9])?\.)*[a-zA-Z0-9]([a-zA-Z0-9\-]*[a-zA-Z0-9])?$`)
)

// ValidateHostmask validates an IRC hostmask in the format nick!user@host per RFC 2812.
func ValidateHostmask(hostmask string) error {
	if hostmask == "" {
		return errors.New("hostmask cannot be empty")
	}

	// Find ! and @ positions
	exclamIdx := strings.Index(hostmask, "!")
	atIdx := strings.Index(hostmask, "@")

	if exclamIdx == -1 {
		return errors.New("hostmask must contain '!' (format: nick!user@host)")
	}
	if atIdx == -1 {
		return errors.New("hostmask must contain '@' (format: nick!user@host)")
	}
	if exclamIdx >= atIdx {
		return errors.New("'!' must come before '@' (format: nick!user@host)")
	}

	nick := hostmask[:exclamIdx]
	user := hostmask[exclamIdx+1 : atIdx]
	host := hostmask[atIdx+1:]

	// Validate nick
	if nick == "" {
		return errors.New("nick cannot be empty")
	}
	if len(nick) > 30 {
		return errors.New("nick too long (max 30 characters)")
	}
	if !nickRegex.MatchString(nick) {
		return errors.New("invalid nick: must start with letter or special char, contain only letters, digits, special chars, or hyphens")
	}

	// Validate user
	if user == "" {
		return errors.New("user cannot be empty")
	}
	if len(user) > 64 {
		return errors.New("user too long (max 64 characters)")
	}
	if !userRegex.MatchString(user) {
		return errors.New("invalid user: must be alphanumeric with optional ~ prefix, allows -_.")
	}

	// Validate host
	if host == "" {
		return errors.New("host cannot be empty")
	}
	if len(host) > 253 {
		return errors.New("host too long (max 253 characters)")
	}

	// Check if it's a valid IP address
	if net.ParseIP(host) != nil {
		return nil
	}

	// Check if it's a valid hostname
	if !hostnameRegex.MatchString(host) {
		return errors.New("invalid host: must be a valid hostname or IP address")
	}

	return nil
}

// nickSpoof matches text imitating the "(nick:x)" identity prefix metald
// prepends to every message before handing it to the model.
var nickSpoof = regexp.MustCompile(`(?i)[\(\[]\s*nick\s*:`)

// SanitizeUserMessage neutralises attempts to forge the identity prefix.
func SanitizeUserMessage(msg string) string {
	return nickSpoof.ReplaceAllStringFunc(msg, func(m string) string {
		// "(nick:" -> "(nick :" - still legible, no longer parses as ours.
		return strings.TrimSuffix(m, ":") + " :"
	})
}

// Structural markers that models use to delimit their own reasoning, tool calls, and conversation
// roles.
var injectionFrame = regexp.MustCompile(
	`(?i)</?\s*(think|thinking|thought|reasoning|scratchpad|` +
		`function|function_call|function_results|tool|tool_call|tool_result|` +
		`system|assistant|user|output|answer)\s*>`)

// ChatML-style turn markers: <|im_start|>, <|im_end|>, <|system|> and friends.
var chatMLMarker = regexp.MustCompile(`<\|[^|>]{0,40}\|>`)

// StripInjectionFrames removes pseudo-structural tags from a user message and reports how many it
// removed.
func StripInjectionFrames(msg string) (string, int) {
	count := len(injectionFrame.FindAllString(msg, -1)) +
		len(chatMLMarker.FindAllString(msg, -1))
	if count == 0 {
		return msg, 0
	}

	cleaned := injectionFrame.ReplaceAllString(msg, " ")
	cleaned = chatMLMarker.ReplaceAllString(cleaned, " ")
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	return cleaned, count
}
