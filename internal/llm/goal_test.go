// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package llm

import (
	"log/slog"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexschlessinger/pollytool/messages"
	"github.com/alexschlessinger/pollytool/tools"

	"B4reMetal/metald/internal/core"
	mocktest "B4reMetal/metald/internal/testing"
)

func TestParseGoalVerdict(t *testing.T) {
	cases := map[string]Verdict{
		"DONE: all five tests pass":                  {"done", "all five tests pass"},
		"**CONTINUE** - zero inputs are untested":    {"continue", "zero inputs are untested"},
		"thinking...\nSTUCK: the sandbox has no gcc": {"stuck", "the sandbox has no gcc"},
		"done": {"done", ""},
	}
	for in, want := range cases {
		if got := parseGoalVerdict(in); got != want {
			t.Errorf("parseGoalVerdict(%q) = %+v, want %+v", in, got, want)
		}
	}
	if got := parseGoalVerdict("looks great to me!"); got.Kind != "continue" {
		t.Errorf("an unreadable verdict must not end a goal: %+v", got)
	}
}

func TestGoalMessageFillsTemplates(t *testing.T) {
	cfg := mocktest.DefaultTestConfig()
	task := core.Task{Kind: core.KindGoal, Objective: "gcd in C", Criteria: "5 tests pass", MaxRuns: 4, Runs: 1,
		Feedback: "zero untested", Todo: []core.TodoItem{{Text: "write", Done: true}}, Notes: "- note"}
	first := goalMessage(cfg, task, true)
	if !strings.Contains(first, "goal: gcd in C") || !strings.Contains(first, "done when: 5 tests pass") || !strings.Contains(first, "rounds: 4") {
		t.Errorf("first round = %q", first)
	}
	later := goalMessage(cfg, task, false)
	for _, want := range []string{"round 2 of 4", "reviewer: zero untested", "1. [x] write", "- note"} {
		if !strings.Contains(later, want) {
			t.Errorf("later round missing %q: %q", want, later)
		}
	}
	if strings.Contains(later, "{") {
		t.Errorf("unfilled placeholder: %q", later)
	}
}

// The reviewer sees what the latest round did - its tool results included - and nothing earlier.
func TestRoundEvidenceIsTheLatestRound(t *testing.T) {
	h := []messages.ChatMessage{
		{Role: messages.MessageRoleSystem, Content: "sys"},
		{Role: messages.MessageRoleUser, Content: "(nick:a) round one"},
		{Role: messages.MessageRoleAssistant, Content: "old answer"},
		{Role: messages.MessageRoleUser, Content: "(nick:a) round two"},
		{Role: messages.MessageRoleAssistant, ToolCalls: []messages.ChatMessageToolCall{{ID: "1", Name: "sandbox__run_code"}}},
		{Role: messages.MessageRoleTool, ToolCallID: "1", Content: "5 passed"},
		{Role: messages.MessageRoleAssistant, Content: "all tests pass"},
	}
	ev := roundEvidence(h)
	if strings.Contains(ev, "old answer") || !strings.Contains(ev, "5 passed") || !strings.Contains(ev, "all tests pass") {
		t.Errorf("evidence = %q", ev)
	}
}

// goalHarness runs endGoalRound against a fake reviewer that answers with verdict.
func goalHarness(t *testing.T, verdict string, task core.Task) (*mocktest.MockChatContext, *core.TaskStore, core.Task) {
	t.Helper()
	var calls atomic.Int32
	srv := summaryServer(t, verdict, &calls, nil)
	sys := mocktest.NewMockSystem()
	session, _ := sys.SessionStore.Get("task/net/1")
	ctx := mocktest.NewMockContext().WithSystem(sys).WithSession(session)
	ctx.GetConfig().API.OpenAIURL = srv.URL
	db, _ := core.Context()
	store, err := core.NewTaskStore(db)
	if err != nil {
		t.Fatal(err)
	}
	task.Kind, task.Objective = core.KindGoal, "gcd"
	started, err := store.Start("net", "#test", "bob", "bob!u@h", core.TaskSpec{Kind: core.KindGoal, Objective: "gcd",
		MaxRuns: task.MaxRuns, Deadline: task.Deadline}, true, core.TaskLimits{})
	if err != nil {
		t.Fatal(err)
	}
	claimed, _ := store.Claim("net")
	if claimed.ID != started.ID {
		t.Fatalf("claimed %d, want %d", claimed.ID, started.ID)
	}
	claimed.Runs, claimed.Spent = task.Runs, task.Spent
	return ctx, store, claimed
}

func endRound(ctx *mocktest.MockChatContext, store *core.TaskStore, task core.Task, r round) core.Task {
	endGoalRound(ctx, ctx.GetConfig(), store, task, "bob", r, slog.Default())
	got, _ := store.Get("net", task.ID)
	return got
}

func TestGoalDoneFinishes(t *testing.T) {
	ctx, store, task := goalHarness(t, "DONE: five tests pass", core.Task{MaxRuns: 5})
	got := endRound(ctx, store, task, round{lines: []string{"here it is"}, ran: time.Minute})
	if got.Status != core.TaskDone || got.Runs != 1 {
		t.Errorf("goal = %s after %d runs", got.Status, got.Runs)
	}
	if !strings.Contains(strings.Join(ctx.Replies, "\n"), "goal #") || !strings.Contains(strings.Join(ctx.Replies, "\n"), "five tests pass") {
		t.Errorf("channel saw %q", ctx.Replies)
	}
}

func TestGoalContinueQueuesAnotherRound(t *testing.T) {
	ctx, store, task := goalHarness(t, "CONTINUE: zero inputs untested", core.Task{MaxRuns: 5})
	lastProgress.Delete(task.ID)
	got := endRound(ctx, store, task, round{lines: []string{"draft"}, ran: time.Minute})
	if got.Status != core.TaskQueued || got.Runs != 1 || got.Feedback != "zero inputs untested" {
		t.Errorf("goal = %+v", got)
	}
	if !got.NextRun.After(time.Now()) {
		t.Error("next round not spaced out")
	}
	if len(ctx.Replies) != 1 || !strings.Contains(ctx.Replies[0], "round 1/5") {
		t.Errorf("progress = %q", ctx.Replies)
	}
	// A second round soon after posts no second progress line.
	got.Status = core.TaskRunning
	endRound(ctx, store, got, round{lines: []string{"draft 2"}, ran: time.Minute})
	if len(ctx.Replies) != 1 {
		t.Errorf("progress not throttled: %q", ctx.Replies)
	}
}

func TestGoalStopsWhenStuckOrOutOfBudget(t *testing.T) {
	ctx, store, task := goalHarness(t, "STUCK: needs a compiler", core.Task{MaxRuns: 5})
	if got := endRound(ctx, store, task, round{lines: []string{"x"}, ran: time.Minute}); got.Status != core.TaskFailed {
		t.Errorf("stuck goal = %s", got.Status)
	}

	ctx, store, task = goalHarness(t, "CONTINUE: more", core.Task{MaxRuns: 2, Runs: 1})
	got := endRound(ctx, store, task, round{lines: []string{"x"}, ran: time.Minute})
	if got.Status != core.TaskFailed || !strings.Contains(got.Result, "used all 2 rounds") {
		t.Errorf("out of rounds = %s %q", got.Status, got.Result)
	}

	ctx, store, task = goalHarness(t, "CONTINUE: more", core.Task{MaxRuns: 20})
	task.Spent = ctx.GetConfig().Session.GoalMaxTime
	if got := endRound(ctx, store, task, round{lines: []string{"x"}, ran: time.Minute}); got.Status != core.TaskFailed {
		t.Errorf("out of time = %s", got.Status)
	}
}

func TestRecurringScheduleIsRescheduled(t *testing.T) {
	sys := mocktest.NewMockSystem()
	session, _ := sys.SessionStore.Get("task/net/2")
	ctx := mocktest.NewMockContext().WithSystem(sys).WithSession(session)
	db, _ := core.Context()
	store, _ := core.NewTaskStore(db)
	past := time.Now().Add(-3 * time.Hour)
	task, err := store.Start("net", "#test", "admin", "a!u@h", core.TaskSpec{Kind: core.KindSchedule, Objective: "news",
		NextRun: past, Interval: time.Hour}, true, core.TaskLimits{})
	if err != nil {
		t.Fatal(err)
	}
	claimed, _ := store.Claim("net")
	endScheduleRun(ctx, store, claimed, "admin", round{lines: []string{"headline"}, ran: time.Second}, slog.Default())
	got, _ := store.Get("net", task.ID)
	if got.Status != core.TaskQueued || !got.NextRun.After(time.Now()) || got.NextRun.After(time.Now().Add(time.Hour)) {
		t.Errorf("recurring schedule = %s, next %v", got.Status, got.NextRun)
	}
	if !strings.Contains(strings.Join(ctx.Replies, "\n"), "headline") {
		t.Errorf("result not posted: %q", ctx.Replies)
	}
}

// Chat never offers the checklist tools; background work never offers the tools that start more
// work, or channel management; a delegate cannot delegate or touch the work's checklist.
func TestToolViews(t *testing.T) {
	var all []tools.Tool
	for _, n := range []string{"task__start", "goal__propose", "todo__set", "task__note", "task__delegate", "irc__kick", "paste__post_gist"} {
		all = append(all, &tools.Func{Name: n})
	}
	base := tools.NewToolRegistry(all)
	names := func(scope toolScope) string { return strings.Join(sortedNames(toolView(base, scope)), ",") }
	if got := names(scopeChat); got != "goal__propose,irc__kick,paste__post_gist,task__start" {
		t.Errorf("chat view = %s", got)
	}
	if got := names(scopeWork); got != "paste__post_gist,task__delegate,task__note,todo__set" {
		t.Errorf("work view = %s", got)
	}
	if got := names(scopeDelegate); got != "paste__post_gist" {
		t.Errorf("delegate view = %s", got)
	}
	base.Register(&tools.Func{Name: "history__search"})
	if !strings.Contains(names(scopeChat), "history__search") {
		t.Error("view not rebuilt when a tool was added")
	}
}

func TestScopeOf(t *testing.T) {
	for name, want := range map[string]toolScope{"examplenet/#chat": scopeChat, "task/examplenet/4": scopeWork,
		"delegate/examplenet/4/1": scopeDelegate} {
		if got := scopeOf(name); got != want {
			t.Errorf("scopeOf(%q) = %d, want %d", name, got, want)
		}
	}
}

func sortedNames(r *tools.ToolRegistry) []string {
	out := registryNames(r)
	sort.Strings(out)
	return out
}
