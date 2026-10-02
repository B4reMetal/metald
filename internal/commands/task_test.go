// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package commands

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"B4reMetal/metald/internal/core"
	mocktest "B4reMetal/metald/internal/testing"
)

func TestParseSchedule(t *testing.T) {
	now := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	in, err := parseSchedule("in", "2h", now)
	if err != nil || !in.NextRun.Equal(now.Add(2*time.Hour)) || in.Interval != 0 {
		t.Errorf("in 2h = %+v %v", in, err)
	}
	every, err := parseSchedule("every", "1d", now)
	if err != nil || every.Interval != 24*time.Hour {
		t.Errorf("every 1d = %+v %v", every, err)
	}
	daily, err := parseSchedule("daily", "09:00", now)
	if err != nil || !daily.NextRun.Equal(time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)) || daily.Interval != 24*time.Hour {
		t.Errorf("daily 09:00 (already past today) = %+v %v", daily, err)
	}
	for _, bad := range [][2]string{{"in", "soon"}, {"in", "10s"}, {"daily", "9am"}, {"weekly", "1d"}} {
		if _, err := parseSchedule(bad[0], bad[1], now); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

func TestSplitCriteria(t *testing.T) {
	o, c := splitCriteria("a gcd in C Done When: five tests pass")
	if o != "a gcd in C" || c != "five tests pass" {
		t.Errorf("split = %q / %q", o, c)
	}
	if o, c := splitCriteria("just a goal"); o != "just a goal" || c != "" {
		t.Errorf("no criteria = %q / %q", o, c)
	}
}

func goalCtx(t *testing.T, nick string, admin bool, args ...string) *mocktest.MockChatContext {
	t.Helper()
	sys := mocktest.NewMockSystem()
	session, _ := sys.SessionStore.Get("net/#test")
	return mocktest.NewMockContext().WithSystem(sys).WithSession(session).WithSource(nick).WithAdmin(admin).WithArgs(args...)
}

// A proposal is accepted by its owner, not by someone else, and a nudge reaches its next round.
func TestGoalAcceptAndNudge(t *testing.T) {
	store, _ := core.Tasks()
	cfg := goalCtx(t, "bob", false).GetConfig()
	p, err := store.Start(cfg.Server.Name, "#test", "bob", "bob!~u@test.host",
		core.TaskSpec{Kind: core.KindGoal, Objective: "gcd", MaxRuns: 3, Proposed: true}, true, core.TaskLimits{})
	if err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(p.ID, 10)

	other := goalCtx(t, "mallory", false, "+goal", "accept", id)
	(&GoalCommand{}).Execute(other)
	if got, _ := store.Get(cfg.Server.Name, p.ID); got.Status != core.TaskProposed {
		t.Errorf("someone else accepted bob's goal: %s", got.Status)
	}

	owner := goalCtx(t, "bob", false, "+goal", "accept", id)
	(&GoalCommand{}).Execute(owner)
	if got, _ := store.Get(cfg.Server.Name, p.ID); got.Status != core.TaskQueued {
		t.Errorf("owner's accept = %s (%q)", got.Status, owner.Replies)
	}

	nudge := goalCtx(t, "bob", false, "+goal", "nudge", id, "try", "abs()")
	(&GoalCommand{}).Execute(nudge)
	if got, _ := store.Get(cfg.Server.Name, p.ID); !strings.Contains(got.Feedback, "from bob: try abs()") {
		t.Errorf("nudge = %q", got.Feedback)
	}
}

func TestScheduleCommandQueues(t *testing.T) {
	ctx := goalCtx(t, "carol", false, "+schedule", "in", "30m", "check", "the", "site")
	(&ScheduleCommand{}).Execute(ctx)
	if len(ctx.Replies) == 0 || !strings.Contains(ctx.Replies[0], "scheduled as #") {
		t.Fatalf("replies = %q", ctx.Replies)
	}
	repeat := goalCtx(t, "dave", false, "+schedule", "every", "1h", "post", "news")
	(&ScheduleCommand{}).Execute(repeat)
	if len(repeat.Replies) == 0 || !strings.Contains(repeat.Replies[0], "only admins") {
		t.Errorf("non-admin repeat = %q", repeat.Replies)
	}
}

// A bare id or "list" after +task reads the queue instead of queuing a job named after it.
func TestTaskSubcommandsDoNotQueue(t *testing.T) {
	store, _ := core.Tasks()
	cfg := goalCtx(t, "alice", false).GetConfig()
	before, _ := store.List(cfg.Server.Name, 50)
	for _, args := range [][]string{{"+task", "3"}, {"+task", "#3"}, {"+task", "list"}, {"+task", "status", "3"}} {
		ctx := goalCtx(t, "alice", false, args...)
		(&TaskCommand{}).Execute(ctx)
		for _, r := range ctx.Replies {
			if strings.Contains(r, "queued as") {
				t.Errorf("%q queued a task: %q", args, ctx.Replies)
			}
		}
	}
	if after, _ := store.List(cfg.Server.Name, 50); len(after) != len(before) {
		t.Errorf("tasks went from %d to %d", len(before), len(after))
	}
}
