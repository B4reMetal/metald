// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package irc

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/alexschlessinger/pollytool/tools"

	"B4reMetal/metald/internal/core"
	mocktest "B4reMetal/metald/internal/testing"
)

func runTool(t *testing.T, tool tools.Tool, ctx context.Context, args tools.Args) string {
	t.Helper()
	out, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("%s: %v", tool.GetName(), err)
	}
	return out
}

// The checklist and note tools work on the task whose conversation they are called from, and
// nowhere else.
func TestTaskOnlyTools(t *testing.T) {
	store, err := core.Tasks()
	if err != nil {
		t.Fatal(err)
	}
	sys := mocktest.NewMockSystem()
	mock := mocktest.NewMockContext().WithSystem(sys)
	task, err := store.Start(mock.GetNetwork(), "#test", "bob", "bob!u@h",
		core.TaskSpec{Kind: core.KindGoal, Objective: "gcd", MaxRuns: 3}, true, core.TaskLimits{})
	if err != nil {
		t.Fatal(err)
	}
	session, _ := sys.SessionStore.Get(fmt.Sprintf("task/%s/%d", mock.GetNetwork(), task.ID))
	ctx := InjectContext(context.Background(), mock.WithSession(session))

	runTool(t, newTodoSetTool(), ctx, tools.Args{"items": []any{"write gcd", "test it"}})
	if out := runTool(t, newTodoUpdateTool(), ctx, tools.Args{"number": float64(1), "done": true}); !strings.Contains(out, "1. [x] write gcd") {
		t.Errorf("update = %q", out)
	}
	if out := runTool(t, newTodoUpdateTool(), ctx, tools.Args{"number": float64(9), "done": true}); !strings.HasPrefix(out, "Error") {
		t.Errorf("out-of-range step accepted: %q", out)
	}
	runTool(t, newTaskNoteTool(), ctx, tools.Args{"note": "zero is a valid input"})

	got, _ := store.Get(mock.GetNetwork(), task.ID)
	if len(got.Todo) != 2 || !got.Todo[0].Done || !strings.Contains(got.Notes, "zero is a valid input") {
		t.Errorf("task after tools = %+v", got)
	}

	chat, _ := sys.SessionStore.Get("net/#test")
	chatCtx := InjectContext(context.Background(), mocktest.NewMockContext().WithSystem(sys).WithSession(chat))
	if out := runTool(t, newTodoSetTool(), chatCtx, tools.Args{"items": []any{"x"}}); !strings.Contains(out, "only works inside") {
		t.Errorf("checklist outside a task = %q", out)
	}
}

func TestStartToolsQueueWork(t *testing.T) {
	sys := mocktest.NewMockSystem()
	session, _ := sys.SessionStore.Get("net/#test")
	ctx := InjectContext(context.Background(), mocktest.NewMockContext().WithSystem(sys).WithSession(session).WithSource("erin"))

	if out := runTool(t, newTaskScheduleTool(), ctx, tools.Args{"objective": "check the site", "delay_minutes": float64(0)}); !strings.HasPrefix(out, "Error") {
		t.Errorf("zero delay accepted: %q", out)
	}
	if out := runTool(t, newTaskScheduleTool(), ctx, tools.Args{"objective": "check the site", "delay_minutes": float64(90)}); !strings.Contains(out, "Scheduled as #") {
		t.Errorf("schedule = %q", out)
	}
	// erin now has one thing in progress, so a proposal is refused by the active limit.
	if out := runTool(t, newGoalProposeTool(), ctx, tools.Args{"objective": "write gcd", "criteria": "tests pass"}); !strings.HasPrefix(out, "Refused") {
		t.Errorf("second active item allowed: %q", out)
	}
}

func TestTextArgStripsLeakedMarkup(t *testing.T) {
	args := tools.Args{"objective": "<parameter=objective>\nwrite a gcd program\n</parameter>"}
	if got := textArg(args, "objective"); got != "write a gcd program" {
		t.Errorf("textArg = %q", got)
	}
	if got := textArg(tools.Args{"objecive": "<parameter=objective>fix the site"}, "objective"); got != "fix the site" {
		t.Errorf("misspelled textArg = %q", got)
	}
}

func TestStartWorkRefusesBareWords(t *testing.T) {
	sys := mocktest.NewMockSystem()
	session, _ := sys.SessionStore.Get("net/#test")
	ctx := InjectContext(context.Background(), mocktest.NewMockContext().WithSystem(sys).WithSession(session).WithSource("eve"))
	for _, objective := range []string{"3", "list", "<parameter=objective></parameter>"} {
		if out := runTool(t, newTaskStartTool(), ctx, tools.Args{"objective": objective}); strings.Contains(out, "Queued") {
			t.Errorf("objective %q queued: %q", objective, out)
		}
	}
}
