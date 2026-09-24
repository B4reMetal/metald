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

// newIrcRemindTool lets the bot schedule a message to be said later in the channel.
func newIrcRemindTool() tools.Tool {
	return &tools.Func{
		Name: "irc__remind",
		Desc: fmt.Sprintf(
			"Schedule a message to be posted in the channel later, pinging "+
				"someone. Use when asked to remind a person about something "+
				"at a later time. The delay must be between 1 minute and %d "+
				"minutes (%d days) - this is a short-term nudge, not a "+
				"calendar, so refuse anything further out instead of "+
				"silently shortening it. THIS TOOL IS THE ONLY WAY TO "+
				"ACTUALLY SET A REMINDER: saying 'i'll remind you' without "+
				"calling it means nothing will ever be sent. Call it first, "+
				"then confirm.",
			int(core.MaxReminderDelay.Minutes()),
			int(core.MaxReminderDelay.Hours()/24),
		),
		Params: schema.Params{
			"nick":    schema.S("The nickname to remind (who gets pinged when it fires)"),
			"minutes": schema.Int("How many minutes from now to send it"),
			"text":    schema.S("What to say to them when it fires"),
		},
		Required: []string{"nick", "minutes", "text"},
		Run: func(ctx context.Context, args tools.Args) (string, error) {
			chatCtx, err := validateContext(ctx)
			if err != nil {
				return "", err
			}

			nick := strings.TrimSpace(args.String("nick"))
			if nick == "" {
				return "", fmt.Errorf("nick must be a non-empty string")
			}
			text := strings.TrimSpace(args.String("text"))
			if text == "" {
				return "", fmt.Errorf("text must be a non-empty string")
			}

			minutes := args.Int("minutes", 0)
			d := time.Duration(minutes) * time.Minute

			// Out-of-range is refused rather than clamped.
			if d < core.MinReminderDelay {
				return fmt.Sprintf(
					"Refused: %d minutes is too soon - the minimum is %d minute(s). Just say it now instead.",
					minutes, int(core.MinReminderDelay.Minutes())), nil
			}
			if d > core.MaxReminderDelay {
				return fmt.Sprintf(
					"Refused: %d minutes is too far out - the maximum is %d minutes (%d days). "+
						"Tell the user you don't do reminders that far ahead.",
					minutes,
					int(core.MaxReminderDelay.Minutes()),
					int(core.MaxReminderDelay.Hours()/24)), nil
			}

			channel := chatCtx.GetConfig().Server.Channel
			r, err := core.Reminders().Add(chatCtx.GetNetwork(), nick, channel, text, chatCtx.GetSource(), d)
			if err != nil {
				return fmt.Sprintf("Refused: %s", err.Error()), nil
			}

			chatCtx.GetLogger().Info("reminder_set",
				"id", r.ID, "nick", r.Nick, "minutes", minutes,
				"due", r.Due.UTC(), "set_by", r.SetBy, "text", r.Text)

			return fmt.Sprintf(
				"Reminder %s set for %s in %d minute(s), at %s UTC. It will be posted in the channel. "+
					"Confirm it briefly in your own voice.",
				r.ID, r.Nick, minutes, r.Due.UTC().Format("15:04")), nil
		},
	}
}

// newIrcRemindersTool lets the bot answer "what reminders are pending" and
// cancel one, without an operator having to read the json file.
func newIrcRemindersTool() tools.Tool {
	return &tools.Func{
		Name: "irc__reminders",
		Desc: "List the reminders currently scheduled, or cancel one by its id. " +
			"Use when someone asks what's pending or wants a reminder called off.",
		Params: schema.Params{
			"cancel_id": schema.S("Optional: the id of a reminder to cancel. Omit to just list them."),
		},
		Run: func(ctx context.Context, args tools.Args) (string, error) {
			chatCtx, err := validateContext(ctx)
			if err != nil {
				return "", err
			}

			if id := strings.TrimSpace(args.String("cancel_id")); id != "" {
				if core.Reminders().Cancel(chatCtx.GetNetwork(), id) {
					chatCtx.GetLogger().Info("reminder_cancelled", "id", id, "by", chatCtx.GetSource())
					return fmt.Sprintf("Cancelled reminder %s", id), nil
				}
				return fmt.Sprintf("No reminder with id %s", id), nil
			}

			pending := core.Reminders().List(chatCtx.GetNetwork())
			if len(pending) == 0 {
				return "No reminders scheduled", nil
			}

			var b strings.Builder
			fmt.Fprintf(&b, "%d reminder(s) pending:", len(pending))
			for _, r := range pending {
				fmt.Fprintf(&b, "\n  %s - %s in %s: %s",
					r.ID, r.Nick, time.Until(r.Due).Round(time.Minute), r.Text)
			}
			return b.String(), nil
		},
	}
}
