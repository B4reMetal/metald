// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package llm

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/alexschlessinger/pollytool/schema"
	"github.com/alexschlessinger/pollytool/sessions"
	"github.com/alexschlessinger/pollytool/tools"

	"B4reMetal/metald/internal/config"
	"B4reMetal/metald/internal/core"
	"B4reMetal/metald/internal/irc"
)

// Background work can hand a focused sub-question to a delegate: a fresh conversation with a small
// budget that answers it and returns the answer to the work, not the channel. It keeps a long goal's
// own conversation free of the digging. A delegate cannot delegate further.

const (
	delegatePrefix     = "delegate/"
	delegateTimeout    = 5 * time.Minute
	delegateIterations = 10
	delegateAnswerMax  = 6000
)

// delegateSlots caps delegates at one at a time: with the work's own round that is two model slots,
// which leaves chat at least one of the default three (maxconcurrent).
var delegateSlots = make(chan struct{}, 1)

var delegateCounter atomic.Uint64

// delegateContext is the calling work's chat context with its own deadline, budget and conversation.
type delegateContext struct {
	irc.ChatContextInterface
	ctx     context.Context
	cfg     *config.Configuration
	session sessions.Session
}

func (d *delegateContext) Deadline() (time.Time, bool)      { return d.ctx.Deadline() }
func (d *delegateContext) Done() <-chan struct{}            { return d.ctx.Done() }
func (d *delegateContext) Err() error                       { return d.ctx.Err() }
func (d *delegateContext) Value(key any) any                { return d.ctx.Value(key) }
func (d *delegateContext) GetConfig() *config.Configuration { return d.cfg }
func (d *delegateContext) GetSession() sessions.Session     { return d.session }

// NewDelegateTool is the task__delegate tool.
func NewDelegateTool() tools.Tool {
	return &tools.Func{
		Name: "task__delegate",
		Desc: "Hand one focused sub-question to a helper and get its answer back: \"find the release date " +
			"of X and cite it\", \"write and sandbox-test a function that does Y\". The helper starts fresh, " +
			"has a few minutes and the same tools, and its answer comes back to you, not the channel. Use it " +
			"to keep digging out of your own conversation. Say exactly what you need back.",
		Params:   schema.Params{"instruction": schema.S("The sub-question, self-contained, and what to return")},
		Required: []string{"instruction"},
		Run:      runDelegate,
	}
}

func runDelegate(ctx context.Context, args tools.Args) (string, error) {
	chatCtx, err := irc.GetIRCContext(ctx)
	if err != nil {
		return "", err
	}
	parent := irc.TaskIDFromSession(chatCtx.GetSession().GetName())
	if parent == 0 {
		return "Error: only background work can delegate", nil
	}
	instruction := strings.TrimSpace(args.String("instruction"))
	if instruction == "" {
		return "Error: no instruction given", nil
	}
	select {
	case delegateSlots <- struct{}{}:
		defer func() { <-delegateSlots }()
	default:
		return "Error: a helper is already busy - do this part yourself, or delegate it after", nil
	}

	store := chatCtx.GetSystem().GetSessionStore()
	key := fmt.Sprintf("%s%s/%d/%d", delegatePrefix, chatCtx.GetNetwork(), parent, delegateCounter.Add(1))
	session, err := store.Get(key)
	if err != nil {
		return "Error: could not start a helper", nil
	}
	defer store.Delete(key)

	cfg := taskConfig(chatCtx.GetConfig(), delegateTimeout)
	cfg.Session.MaxIterations = delegateIterations
	md := session.GetMetadata()
	md.SystemPrompt = strings.TrimSpace(cfg.Bot.Prompt) + "\n\n" + strings.TrimSpace(cfg.Bot.DelegatePrompt)
	session.SetMetadata(md)
	session.Clear()

	dctx, cancel := context.WithTimeout(ctx, delegateTimeout)
	defer cancel()
	d := &delegateContext{ChatContextInterface: chatCtx, ctx: dctx, cfg: cfg, session: session}
	chatCtx.GetLogger().Info("delegate_started", "task", parent, "instruction", truncate(instruction, 200))

	start := time.Now()
	var lines []string
	core.WithRequestLock(d, "delegate:"+key, "delegate", func() {
		out, err := Complete(d, fmt.Sprintf("(nick:%s) %s", chatCtx.GetSource(), irc.SanitizeUserMessage(instruction)))
		if err != nil {
			return
		}
		for line := range out {
			if strings.TrimSpace(line) != "" {
				lines = append(lines, line)
			}
		}
	}, nil)
	chatCtx.GetLogger().Info("delegate_finished", "task", parent, "lines", len(lines),
		"duration_ms", time.Since(start).Milliseconds())

	answer := strings.Join(lines, "\n")
	if answer == "" {
		return "The helper came back with nothing (it may have run out of time). Do this part yourself.", nil
	}
	if len(answer) > delegateAnswerMax {
		answer = answer[:delegateAnswerMax] + "\n... (cut)"
	}
	return "Helper's answer (check it before relying on it):\n" + answer, nil
}
