// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package commands

import (
	"fmt"
	"strings"

	"B4reMetal/metald/internal/core"
	"B4reMetal/metald/internal/irc"
)

// suspicionShown bounds how many nicks one +suspicion lists.
const suspicionShown = 10

// SuspicionCommand shows this network's decaying per-speaker suspicion scores.
type SuspicionCommand struct{}

func (c *SuspicionCommand) Name() string    { return "+suspicion" }
func (c *SuspicionCommand) AdminOnly() bool { return false }

func (c *SuspicionCommand) Execute(ctx irc.ChatContextInterface) {
	network := ctx.GetNetwork()
	if args := ctx.GetArgs(); len(args) > 1 {
		nick := args[1]
		ctx.Reply(fmt.Sprintf("%s: %.1f (quarantine at %.1f)", nick, core.Suspicions().Score(network, nick), core.SuspicionQuarantine))
		return
	}
	scores := core.Suspicions().Snapshot(network)
	if len(scores) == 0 {
		ctx.Reply("no one has a suspicion score right now")
		return
	}
	parts := make([]string, 0, suspicionShown)
	for i, s := range scores {
		if i == suspicionShown {
			parts = append(parts, fmt.Sprintf("+%d more", len(scores)-suspicionShown))
			break
		}
		parts = append(parts, fmt.Sprintf("%s %.1f", s.Nick, s.Score))
	}
	ctx.Reply(fmt.Sprintf("suspicion (quarantine at %.1f, halves every 10m): %s", core.SuspicionQuarantine, strings.Join(parts, ", ")))
}
