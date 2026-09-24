// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"strings"

	"github.com/alexschlessinger/pollytool/messages"
	"github.com/alexschlessinger/pollytool/sessions"
)

// speakerPrefix is how a user message is attributed in the history.
func speakerPrefix(nick string) string {
	return "(nick:" + strings.ToLower(nick) + ")"
}

// QuarantineSpeaker removes one speaker's turns from a session and returns how many messages went.
func QuarantineSpeaker(session sessions.Session, nick string) int {
	if session == nil || nick == "" {
		return 0
	}

	mu := CommitLock(session)
	mu.Lock()
	defer mu.Unlock()

	history := session.GetHistory()
	if len(history) == 0 {
		return 0
	}

	prefix := speakerPrefix(nick)

	var keep []messages.ChatMessage
	var system []messages.ChatMessage
	dropping := false
	dropped := 0

	for _, m := range history {
		switch {
		case m.Role == messages.MessageRoleSystem:
			// The system message is the prompt, not conversation. Held
			// separately so it survives regardless of where it sits.
			system = append(system, m)
			dropping = false
			continue

		case m.Role == messages.MessageRoleUser:
			// A user message always ends the previous turn, whoever it
			// belongs to, and decides whether this one is being dropped.
			dropping = strings.HasPrefix(strings.ToLower(strings.TrimSpace(m.Content)), prefix)
		}

		if dropping {
			dropped++
			continue
		}
		keep = append(keep, m)
	}

	if dropped == 0 {
		return 0
	}

	session.Clear()
	if len(session.GetHistory()) == 0 {
		for _, m := range system {
			session.AddMessage(m)
		}
	}
	for _, m := range keep {
		session.AddMessage(m)
	}
	return dropped
}
