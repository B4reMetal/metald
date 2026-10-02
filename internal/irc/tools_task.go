// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package irc

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/alexschlessinger/pollytool/schema"
	"github.com/alexschlessinger/pollytool/tools"

	"B4reMetal/metald/internal/config"
	"B4reMetal/metald/internal/core"
)

// Background work: tasks, goals and schedules. Chat tools start work; the todo and note tools exist
// only inside a task's own conversation (see TaskOnlyTools).

// TaskSessionPrefix names the conversation a piece of background work runs in: task/<network>/<id>.
const TaskSessionPrefix = "task/"

// TaskIDFromSession returns the task a session belongs to, or 0 for an ordinary conversation.
func TaskIDFromSession(name string) int64 {
	if !strings.HasPrefix(name, TaskSessionPrefix) {
		return 0
	}
	i := strings.LastIndexByte(name, '/')
	id, _ := strconv.ParseInt(name[i+1:], 10, 64)
	return id
}

// ChatOnlyTools start background work, so they are withheld inside it.
var ChatOnlyTools = map[string]bool{"task__start": true, "task__schedule": true, "goal__propose": true}

// TaskOnlyTools manage the work a task is doing, so they exist only inside it.
var TaskOnlyTools = map[string]bool{"todo__set": true, "todo__update": true, "task__note": true, "task__delegate": true}

// NotInTasks are channel-management tools background work never gets.
var NotInTasks = map[string]bool{"irc__op": true, "irc__kick": true, "irc__ban": true, "irc__topic": true,
	"irc__mode_set": true, "irc__invite": true, "irc__ignore": true}

// TaskToolset is what enabling task__start in config brings with it.
var TaskToolset = []string{"task__start", "task__schedule", "goal__propose", "todo__set", "todo__update", "task__note", "task__delegate"}

// StartWork records background work for whoever sent ctx's message, in the channel it came from.
// Screened nicks, private messages and background work itself cannot start any; non-admins are held
// to the configured limits.
func StartWork(ctx ChatContextInterface, spec core.TaskSpec) (core.Task, error) {
	cfg := ctx.GetConfig()
	if TaskIDFromSession(ctx.GetSession().GetName()) != 0 {
		return core.Task{}, fmt.Errorf("%w: background work can't start more of itself - do it here", core.ErrTaskQuota)
	}
	if ctx.IsPrivate() {
		return core.Task{}, fmt.Errorf("%w: background work starts from a channel", core.ErrTaskQuota)
	}
	if len(strings.Fields(spec.Objective)) < minObjectiveWords {
		return core.Task{}, fmt.Errorf("%w: say what the work should do, in a few words", core.ErrTaskQuota)
	}
	if core.NickListed(cfg.Bot.ScreenNicks, ctx.GetSource()) {
		return core.Task{}, fmt.Errorf("%w: not available to %s", core.ErrTaskQuota, ctx.GetSource())
	}
	store, err := core.Tasks()
	if err != nil {
		return core.Task{}, err
	}
	task, err := store.Start(ctx.GetNetwork(), ctx.GetTarget(), ctx.GetSource(), ctx.GetSourceMask(), spec,
		ctx.IsAdmin(), core.TaskLimits{MaxActive: cfg.Session.TaskMaxActive, DailyLimit: cfg.Session.TaskDailyLimit})
	if err == nil {
		ctx.GetLogger().Info("task_queued", "task", task.ID, "kind", task.Kind, "status", task.Status,
			"owner", task.Owner, "objective", truncateLog(spec.Objective, 200))
	}
	return task, err
}

// minObjectiveWords stops a mistyped subcommand ("+task list", "+task 3") from queuing a job.
const minObjectiveWords = 2

// GoalSpec is a goal with the configured budget.
func GoalSpec(cfg *config.Configuration, objective, criteria string) core.TaskSpec {
	return core.TaskSpec{Kind: core.KindGoal, Objective: objective, Criteria: criteria,
		MaxRuns: cfg.Session.GoalMaxRuns, Deadline: time.Now().Add(cfg.Session.GoalDeadline)}
}

func truncateLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// workRefusal turns a refused start into a tool result the model can pass on.
func workRefusal(err error) string {
	return "Refused: " + strings.TrimPrefix(err.Error(), core.ErrTaskQuota.Error()+": ") +
		". Tell them briefly; do it now instead if it's small enough."
}

// textArg reads a string argument, or the one text argument the model sent under a misspelled name
// ("objecive" has happened).
func textArg(args tools.Args, name string) string {
	if v := unwrapParam(args.String(name)); v != "" {
		return v
	}
	var only string
	for k := range args {
		if _, isNumber := args[k].(float64); isNumber {
			continue
		}
		if v := unwrapParam(args.String(k)); v != "" {
			if only != "" {
				return ""
			}
			only = v
		}
	}
	return only
}

// leakedParamTag matches tool-call markup the model sometimes writes inside an argument's value.
var leakedParamTag = regexp.MustCompile(`</?parameter(=[^>]*)?>`)

func unwrapParam(s string) string {
	return strings.TrimSpace(leakedParamTag.ReplaceAllString(s, ""))
}

// taskObjective is textArg for "objective".
func taskObjective(args tools.Args) string { return textArg(args, "objective") }

func startTool(ctx context.Context, spec core.TaskSpec, queued string) (string, error) {
	chatCtx, err := validateContext(ctx)
	if err != nil {
		return "", err
	}
	if spec.Objective == "" {
		return "Error: no objective given. Call the tool again with the whole job in \"objective\".", nil
	}
	task, err := StartWork(chatCtx, spec)
	if errors.Is(err, core.ErrTaskQuota) {
		return workRefusal(err), nil
	}
	if err != nil {
		chatCtx.GetLogger().Error("task_start_failed", "error", err.Error())
		return "Error: could not queue it", nil
	}
	return fmt.Sprintf(queued, task.ID), nil
}

func newTaskStartTool() tools.Tool {
	return &tools.Func{
		Name: "task__start",
		Desc: "Hand a job to yourself as a background task instead of doing it now. Use it for work that " +
			"needs many steps or a long time - writing and testing code, research across several " +
			"searches, anything you'd otherwise run out of time on - or when someone says to take your " +
			"time. You get far more time and steps there, the channel isn't held up, and the result is " +
			"posted to the channel when it's done. Write the objective so it stands on its own: what to " +
			"make or find, for whom, and what counts as done. Tell the person briefly that it's queued.",
		Params: schema.Params{
			"objective": schema.S("The whole job, self-contained: what to do and what a finished result looks like"),
		},
		Required: []string{"objective"},
		Run: func(ctx context.Context, args tools.Args) (string, error) {
			return startTool(ctx, core.TaskSpec{Kind: core.KindTask, Objective: taskObjective(args)},
				"Queued as task #%d. The result will be posted in the channel when it's done. "+
					"Tell them it's queued, in your own voice - don't start the work yourself.")
		},
	}
}

func newTaskScheduleTool() tools.Tool {
	return &tools.Func{
		Name: "task__schedule",
		Desc: "Do something later, once: \"in two hours, check whether that site is back and tell bob\". " +
			"At that time you run the objective as a background task and post the result in the channel. " +
			"For a plain reminder with nothing to work out, use irc__remind instead. Repeating schedules " +
			"are set up by admins with +schedule, not by you.",
		Params: schema.Params{
			"objective":     schema.S("What to do at that time, self-contained, including who to tell"),
			"delay_minutes": schema.Int("How many minutes from now (1 to 4320)"),
		},
		Required: []string{"objective", "delay_minutes"},
		Run: func(ctx context.Context, args tools.Args) (string, error) {
			delay := args.Int("delay_minutes", 0)
			if delay < 1 || delay > 4320 {
				return "Error: delay_minutes must be between 1 and 4320 (three days)", nil
			}
			when := time.Now().Add(time.Duration(delay) * time.Minute)
			return startTool(ctx, core.TaskSpec{Kind: core.KindSchedule, Objective: taskObjective(args), NextRun: when},
				"Scheduled as #%d for "+when.UTC().Format("15:04 UTC")+". Tell them when it will happen, briefly.")
		},
	}
}

func newGoalProposeTool() tools.Tool {
	return &tools.Func{
		Name: "goal__propose",
		Desc: "Propose a goal: a job you keep working on in the background, round after round, until a " +
			"reviewer confirms it meets its success criteria (or its budget runs out). Use it for things " +
			"that need iterating - code that must pass tests, a result that must meet stated conditions. " +
			"It does not start until the person accepts it with +goal accept <id>, so tell them that. " +
			"For a one-shot job use task__start instead.",
		Params: schema.Params{
			"objective": schema.S("What to achieve, self-contained"),
			"criteria":  schema.S("How to tell it's done, concretely checkable: tests that pass, conditions that hold"),
		},
		Required: []string{"objective", "criteria"},
		Run: func(ctx context.Context, args tools.Args) (string, error) {
			chatCtx, err := validateContext(ctx)
			if err != nil {
				return "", err
			}
			spec := GoalSpec(chatCtx.GetConfig(), textArg(args, "objective"), args.String("criteria"))
			spec.Proposed = true
			return startTool(ctx, spec,
				"Proposed as goal #%d. It starts only when they say +goal accept <id> - tell them that, briefly, "+
					"with the id. Don't start the work yourself.")
		},
	}
}

// currentTask finds the task whose conversation ctx is in.
func currentTask(ctx context.Context) (ChatContextInterface, *core.TaskStore, core.Task, string) {
	chatCtx, err := validateContext(ctx)
	if err != nil {
		return nil, nil, core.Task{}, "Error: " + err.Error()
	}
	id := TaskIDFromSession(chatCtx.GetSession().GetName())
	if id == 0 {
		return nil, nil, core.Task{}, "Error: this only works inside a background task"
	}
	store, err := core.Tasks()
	if err != nil {
		return nil, nil, core.Task{}, "Error: task store unavailable"
	}
	task, ok := store.Get(chatCtx.GetNetwork(), id)
	if !ok {
		return nil, nil, core.Task{}, "Error: task not found"
	}
	return chatCtx, store, task, ""
}

// FormatTodo renders a checklist for the model or the channel.
func FormatTodo(items []core.TodoItem) string {
	if len(items) == 0 {
		return "(no checklist)"
	}
	var b strings.Builder
	for i, it := range items {
		mark := " "
		if it.Done {
			mark = "x"
		}
		fmt.Fprintf(&b, "%d. [%s] %s\n", i+1, mark, it.Text)
	}
	return strings.TrimSpace(b.String())
}

func newTodoSetTool() tools.Tool {
	return &tools.Func{
		Name: "todo__set",
		Desc: "Write this task's checklist: the steps to get it done, in order. Replaces the old list. " +
			"Later rounds see it, so keep it current.",
		Params: schema.Params{
			"items": map[string]any{"type": "array", "items": map[string]any{"type": "string"},
				"description": "The steps, in order"},
		},
		Required: []string{"items"},
		Run: func(ctx context.Context, args tools.Args) (string, error) {
			_, store, task, errMsg := currentTask(ctx)
			if errMsg != "" {
				return errMsg, nil
			}
			var items []core.TodoItem
			for _, s := range args.StringSlice("items") {
				if strings.TrimSpace(s) != "" {
					items = append(items, core.TodoItem{Text: s})
				}
			}
			if err := store.SetTodo(task.ID, items); err != nil {
				return "Error: could not save the checklist", nil
			}
			return "Checklist saved:\n" + FormatTodo(items), nil
		},
	}
}

func newTodoUpdateTool() tools.Tool {
	return &tools.Func{
		Name: "todo__update",
		Desc: "Tick off (or untick) one step of this task's checklist by its number.",
		Params: schema.Params{
			"number": schema.Int("The step's number, from 1"),
			"done":   map[string]any{"type": "boolean", "description": "true when the step is finished"},
		},
		Required: []string{"number", "done"},
		Run: func(ctx context.Context, args tools.Args) (string, error) {
			_, store, task, errMsg := currentTask(ctx)
			if errMsg != "" {
				return errMsg, nil
			}
			n := args.Int("number", 0)
			if n < 1 || n > len(task.Todo) {
				return fmt.Sprintf("Error: there is no step %d; the checklist has %d", n, len(task.Todo)), nil
			}
			task.Todo[n-1].Done = args.Bool("done")
			if err := store.SetTodo(task.ID, task.Todo); err != nil {
				return "Error: could not save the checklist", nil
			}
			return FormatTodo(task.Todo), nil
		},
	}
}

func newTaskNoteTool() tools.Tool {
	return &tools.Func{
		Name: "task__note",
		Desc: "Write down something this task's later rounds need to know: a finding, a decision, what " +
			"failed and why, where a paste is. Notes survive restarts; keep each one short.",
		Params:   schema.Params{"note": schema.S("One short note")},
		Required: []string{"note"},
		Run: func(ctx context.Context, args tools.Args) (string, error) {
			_, store, task, errMsg := currentTask(ctx)
			if errMsg != "" {
				return errMsg, nil
			}
			note := textArg(args, "note")
			if note == "" {
				return "Error: empty note", nil
			}
			if err := store.AddNote(task.ID, note); err != nil {
				return "Error: could not save the note", nil
			}
			return "Noted.", nil
		},
	}
}
