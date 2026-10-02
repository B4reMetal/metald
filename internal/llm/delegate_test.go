// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package llm

import (
	"context"
	"strings"
	"testing"

	"github.com/alexschlessinger/pollytool/tools"

	"B4reMetal/metald/internal/irc"
	mocktest "B4reMetal/metald/internal/testing"
)

func TestDelegateAnswersBackToTheWork(t *testing.T) {
	sys := mocktest.NewMockSystem()
	sys.LLM = &mocktest.MockLLM{Responses: []string{"the release date is 2004-11-16"}}
	session, _ := sys.SessionStore.Get("task/net/7")
	chatCtx := mocktest.NewMockContext().WithSystem(sys).WithSession(session).WithSource("bob")
	ctx := irc.InjectContext(context.Background(), chatCtx)

	out, err := runDelegate(ctx, tools.Args{"instruction": "when did half-life 2 come out?"})
	if err != nil || !strings.Contains(out, "2004-11-16") || !strings.HasPrefix(out, "Helper's answer") {
		t.Fatalf("delegate = %q, %v", out, err)
	}
	if req := sys.LLM.(*mocktest.MockLLM).LastRequest(); req == nil || !strings.Contains(req.Messages[0].Content, chatCtx.GetConfig().Bot.DelegatePrompt) {
		t.Error("the helper did not get the delegate prompt")
	}
	names, _ := sys.SessionStore.List()
	for _, n := range names {
		if strings.HasPrefix(n, delegatePrefix) {
			t.Errorf("helper conversation %s left behind", n)
		}
	}
	if len(chatCtx.Replies) != 0 {
		t.Errorf("a helper wrote to the channel: %q", chatCtx.Replies)
	}
}

func TestDelegateGuards(t *testing.T) {
	sys := mocktest.NewMockSystem()
	chat, _ := sys.SessionStore.Get("net/#test")
	ctx := irc.InjectContext(context.Background(), mocktest.NewMockContext().WithSystem(sys).WithSession(chat))
	if out, _ := runDelegate(ctx, tools.Args{"instruction": "x"}); !strings.Contains(out, "only background work") {
		t.Errorf("delegated from chat: %q", out)
	}

	work, _ := sys.SessionStore.Get("task/net/8")
	wctx := irc.InjectContext(context.Background(), mocktest.NewMockContext().WithSystem(sys).WithSession(work))
	delegateSlots <- struct{}{}
	out, _ := runDelegate(wctx, tools.Args{"instruction": "x"})
	<-delegateSlots
	if !strings.Contains(out, "already busy") {
		t.Errorf("a second helper ran at once, which would leave chat no model slot: %q", out)
	}
}
