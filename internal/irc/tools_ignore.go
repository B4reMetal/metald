// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package irc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/alexschlessinger/pollytool/schema"
	"github.com/alexschlessinger/pollytool/tools"

	"B4reMetal/metald/internal/core"
)

// maxSelfIgnoreDuration caps how long the bot may mute someone on its own initiative.
const maxSelfIgnoreDuration = time.Hour

// newIrcIgnoreTool lets the bot mute someone itself, without an operator having to run +ignore.
func newIrcIgnoreTool() tools.Tool {
	return &tools.Func{
		Name: "irc__ignore",
		Desc: "Stop responding to a specific user for a while, when they are " +
			"spamming, flooding, repeatedly baiting you, or otherwise not worth " +
			"replying to. They will get no response at all until it expires. " +
			"Use sparingly and only when someone is genuinely being a problem - " +
			"not merely because you dislike a question or someone disagrees " +
			"with you. Admins cannot be ignored. " +
			"THIS TOOL IS THE ONLY WAY TO ACTUALLY IGNORE ANYONE: merely " +
			"writing 'i'm ignoring you' in a reply does nothing at all and " +
			"you will keep answering them. If you intend to ignore someone, " +
			"call this first, then say so.",
		Params: schema.Params{
			"nick":    schema.S("The nickname to stop responding to"),
			"minutes": schema.Int("How many minutes to ignore them, 1 to 60"),
			"reason":  schema.S("Brief reason, for the operator's logs"),
		},
		Required: []string{"nick", "minutes", "reason"},
		Run: func(ctx context.Context, args tools.Args) (string, error) {
			chatCtx, err := validateContext(ctx)
			if err != nil {
				return "", err
			}

			nick := strings.TrimSpace(args.String("nick"))
			if nick == "" {
				return "", fmt.Errorf("nick must be a non-empty string")
			}

			// Never let the bot mute an operator - they need to stay able to
			// reach it in order to undo this.
			if isAdminNick(chatCtx, nick) {
				return fmt.Sprintf("%s is an admin and cannot be ignored", nick), nil
			}
			if strings.EqualFold(nick, chatCtx.GetBotNick()) {
				return "Refusing to ignore myself", nil
			}

			minutes := args.Int("minutes", 10)
			if minutes < 1 {
				minutes = 1
			}
			if d := time.Duration(minutes) * time.Minute; d > maxSelfIgnoreDuration {
				minutes = int(maxSelfIgnoreDuration / time.Minute)
			}
			duration := time.Duration(minutes) * time.Minute

			expiry := core.Ignores().Add(chatCtx.GetNetwork(), nick, duration)
			chatCtx.GetLogger().Info("self_ignore",
				"nick", nick,
				"minutes", minutes,
				"reason", args.String("reason"),
				"requested_by", chatCtx.GetSource(),
				"until", expiry.UTC(),
			)

			return fmt.Sprintf(
				"Now ignoring %s for %d minute(s). They will get no replies until %s UTC. "+
					"Mention in your reply that you're done responding to them.",
				nick, minutes, expiry.UTC().Format("15:04")), nil
		},
	}
}

// isAdminNick reports whether nick belongs to a configured admin, matching
// the nick portion of each admin hostmask (nick!ident@host).
func isAdminNick(chatCtx ChatContextInterface, nick string) bool {
	for _, mask := range chatCtx.GetConfig().Bot.Admins {
		adminNick, _, found := strings.Cut(mask, "!")
		if !found {
			adminNick = mask
		}
		if strings.EqualFold(adminNick, nick) {
			return true
		}
	}
	return false
}
