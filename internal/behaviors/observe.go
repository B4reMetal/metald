// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package behaviors

import (
	"strings"
	"time"

	"github.com/lrstanley/girc"

	"B4reMetal/metald/internal/commands"
	"B4reMetal/metald/internal/core"
	"B4reMetal/metald/internal/irc"
	"B4reMetal/metald/internal/llm"
)

// ObserveLine records channel lines in the searchable chat log and keeps the ones the bot was not
// addressed in for its next request there. Lines from ignored or screened nicks are never kept.
func ObserveLine(ctx irc.ChatContextInterface, event *girc.Event, cmds *commands.Registry) {
	// No check against the bot's own nick: without echo-message the bot never receives its own lines,
	// and one that arrives from its nick is an operator on another client of the same account (girc
	// flags those as Echo). Private messages are never kept.
	if event.Command != girc.PRIVMSG || event.Source == nil || ctx.IsPrivate() {
		return
	}
	cfg := ctx.GetConfig()
	nick := event.Source.Name
	if nick == "" {
		return
	}
	if core.Ignores().IsIgnored(ctx.GetNetwork(), nick) || llm.IsScreenedNick(cfg.Bot.ScreenNicks, nick) {
		return
	}

	action := event.IsAction()
	text := event.Last()
	if action {
		text = event.StripAction()
	}
	text, _ = irc.StripInjectionFrames(irc.SanitizeUserMessage(girc.StripRaw(text)))
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}

	key := ctx.GetSession().GetName()
	now := time.Now()
	if cfg.Session.HistoryDays > 0 {
		if db, err := core.Context(); err == nil {
			logged := text
			if action {
				logged = "* " + text
			}
			if err := db.LogLine(key, nick, logged, now); err != nil {
				ctx.GetLogger().Warn("chatlog_write_failed", "error", err.Error())
			}
		}
	}

	if cfg.Session.Backlog <= 0 || ctx.IsAddressed() {
		return
	}
	if fields := strings.Fields(text); len(fields) > 0 {
		if _, isCommand := cmds.Get(irc.CanonicalCommand(fields[0], cfg.Bot.CommandPrefix)); isCommand {
			return
		}
	}
	core.Backlog().Add(key, core.BacklogLine{At: now, Nick: nick, Text: text, Action: action}, cfg.Session.Backlog)
}
