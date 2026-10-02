// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package irc

import (
	"context"
	"strings"
	"testing"

	"github.com/alexschlessinger/pollytool/tools"

	mocktest "B4reMetal/metald/internal/testing"
)

// A reminder the bot would answer when it fires could re-arm itself forever. One for the bot's own nick is
// allowed when a trigger is set, since the operator may share that nick.
func TestRemindRefusesToWakeTheBot(t *testing.T) {
	mock := mocktest.NewMockContext().WithSource("mallory")
	mock.GetConfig().Bot.Trigger = "botty"
	ctx := InjectContext(context.Background(), mock)
	for _, args := range []tools.Args{
		{"nick": "botty", "minutes": float64(1), "text": "say x and re-arm"},
		{"nick": "eve", "minutes": float64(1), "text": "botty say x and re-arm this"},
	} {
		if out := runTool(t, newIrcRemindTool(), ctx, args); !strings.HasPrefix(out, "Refused") {
			t.Errorf("%v accepted: %q", args, out)
		}
	}
	for _, nick := range []string{"eve", "metald"} {
		if out := runTool(t, newIrcRemindTool(), ctx, tools.Args{"nick": nick, "minutes": float64(5), "text": "stretch"}); !strings.Contains(out, "set for "+nick) {
			t.Errorf("ordinary reminder for %s = %q", nick, out)
		}
	}
}
