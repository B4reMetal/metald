// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package irc

import (
	"context"

	"github.com/alexschlessinger/pollytool/sessions"
	"github.com/lrstanley/girc"

	"B4reMetal/metald/internal/config"
	"B4reMetal/metald/internal/core"
)

// taskContext is a chat context for a background task: it speaks in the channel the task came from,
// as the person who asked (their hostmask, so admin checks still hold), but keeps its own
// conversation so the channel's history is untouched.
type taskContext struct {
	ChatContextInterface
	session sessions.Session
}

func (t *taskContext) GetSession() sessions.Session { return t.session }

// NewTaskContext builds the context a background task runs in. cfg's API timeout bounds the whole
// task.
func NewTaskContext(parent context.Context, cfg *config.Configuration, sys core.System, client *girc.Client,
	channel, ownerMask, text, sessionKey string, fatalCh chan<- error) (ChatContextInterface, context.CancelFunc, error) {
	src := girc.ParseSource(ownerMask)
	if src == nil || src.Name == "" {
		src = &girc.Source{Name: ownerMask}
	}
	ev := &girc.Event{Command: girc.PRIVMSG, Source: src, Params: []string{channel, text}}
	ctx, cancel := NewChatContext(parent, cfg, sys, client, ev, fatalCh)
	session, err := sys.GetSessionStore().Get(sessionKey)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	return &taskContext{ChatContextInterface: ctx, session: session}, cancel, nil
}
