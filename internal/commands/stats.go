// Copyright (C) 2023-2026 Alex Schlessinger and soulshack contributors
// Modified 2026 by BareMetal
// SPDX-License-Identifier: GPL-3.0-only

package commands

import (
	"fmt"
	"strings"
	"time"

	"B4reMetal/metald/internal/irc"

	"github.com/alexschlessinger/pollytool/messages"
	"github.com/alexschlessinger/pollytool/sessions"
)

// StatsCommand handles the +stats command for showing session statistics
type StatsCommand struct{}

func (c *StatsCommand) Name() string    { return "+stats" }
func (c *StatsCommand) AdminOnly() bool { return false }

func (c *StatsCommand) Execute(ctx irc.ChatContextInterface) {
	session := ctx.GetSession()
	history := session.GetHistory()

	// History size by its text, the same measure the recap folder uses.
	historySize := 0
	for _, msg := range history {
		if msg.Role != messages.MessageRoleSystem {
			historySize += sessions.EstimateTokens(msg)
		}
	}
	maxContext := ctx.GetConfig().Session.MaxContext

	// Calculate token breakdown
	totalInputTokens := 0
	totalOutputTokens := 0
	totalEstimated := 0

	// Track participants (IRC-specific)
	participants := make(map[string]bool)

	for _, msg := range history {
		// Token counting
		input := msg.GetInputTokens()
		output := msg.GetOutputTokens()

		if input > 0 || output > 0 {
			totalInputTokens += input
			totalOutputTokens += output
		} else {
			// Using estimation fallback
			estimated := sessions.EstimateTokens(msg)
			totalEstimated += estimated
		}

		// Track participants from user messages
		if msg.Role == messages.MessageRoleUser {
			// Extract nick from content if present
			// Format: "(nick:username) message"
			if strings.HasPrefix(msg.Content, "(nick:") {
				end := strings.Index(msg.Content, ")")
				if end > 6 {
					parts := strings.SplitN(msg.Content[6:end], ":", 2)
					if len(parts) > 0 {
						nick := parts[0]
						participants[nick] = true
					}
				}
			}
		}
	}

	// Get message counts and tool calls using new interface methods
	messageCounts := session.GetMessageCounts()

	// Idle conversations are folded into the recap, not expired.
	ttlStr := "never folded"
	if idle := ctx.GetConfig().Session.TTL; idle > 0 {
		ttlStr = fmt.Sprintf("recap after %s idle", formatDuration(idle))
	}

	capacityStr := fmt.Sprintf("~%d tokens", historySize)
	if maxContext > 0 {
		capacityStr = fmt.Sprintf("%.1f%% of %d", float64(historySize)*100/float64(maxContext), maxContext)
	}

	// Build response in simple format
	response := fmt.Sprintf(
		"token input: %d, "+
			"token output: %d, "+
			"context capacity: %s, "+
			"messages: %d (user: %d, assistant: %d, tool: %d), "+
			"participants: %d, "+
			"ttl: %s",
		totalInputTokens,
		totalOutputTokens,
		capacityStr,
		len(history),
		messageCounts[string(messages.MessageRoleUser)],
		messageCounts[string(messages.MessageRoleAssistant)],
		messageCounts[string(messages.MessageRoleTool)],
		len(participants),
		ttlStr,
	)

	ctx.Reply(response)
}

// formatDuration formats a duration into a human-readable string
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	} else if d < time.Hour {
		return fmt.Sprintf("%.0fm", d.Minutes())
	} else {
		hours := int(d.Hours())
		minutes := int(d.Minutes()) % 60
		if minutes > 0 {
			return fmt.Sprintf("%dh %dm", hours, minutes)
		}
		return fmt.Sprintf("%dh", hours)
	}
}
