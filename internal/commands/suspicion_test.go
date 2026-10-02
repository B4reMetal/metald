// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package commands

import (
	"strings"
	"testing"

	"B4reMetal/metald/internal/core"
	mocktest "B4reMetal/metald/internal/testing"
)

func TestSuspicionCommand(t *testing.T) {
	ctx := mocktest.NewMockContext().WithArgs("+suspicion")
	network := ctx.GetNetwork()
	core.Suspicions().Clear(network, "mallory")
	core.Suspicions().Clear(network, "eve")
	core.Suspicions().Add(network, "mallory", core.SignalReplyDenied)
	core.Suspicions().Add(network, "eve", core.SignalToolSyntax)
	core.Suspicions().Add("elsewhere", "dave", core.SignalReplyDenied)

	(&SuspicionCommand{}).Execute(ctx)
	got := strings.Join(ctx.Replies, "\n")
	if !strings.Contains(got, "mallory 2.0, eve 0.5") || strings.Contains(got, "dave") {
		t.Errorf("list = %q; want this network's scores, highest first", got)
	}

	one := mocktest.NewMockContext().WithArgs("+suspicion", "mallory")
	(&SuspicionCommand{}).Execute(one)
	if len(one.Replies) != 1 || !strings.HasPrefix(one.Replies[0], "mallory: 2.0") {
		t.Errorf("one nick = %q", one.Replies)
	}
}
