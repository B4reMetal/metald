// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package commands

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"B4reMetal/metald/internal/core"
	"B4reMetal/metald/internal/irc"
	"B4reMetal/metald/internal/llm"
)

// Background work from the channel: +task (one job), +goal (rounds until a reviewer passes it),
// +schedule (later, or repeating) and +tasks (the list). All of it is the same work queue.

// startWork queues spec and tells the channel, or says why not.
func startWork(ctx irc.ChatContextInterface, spec core.TaskSpec, queued string) {
	task, err := irc.StartWork(ctx, spec)
	if errors.Is(err, core.ErrTaskQuota) {
		ctx.Reply(strings.TrimPrefix(err.Error(), core.ErrTaskQuota.Error()+": "))
		return
	}
	if err != nil {
		ctx.GetLogger().Error("task_start_failed", "error", err.Error())
		ctx.Reply("couldn't queue that")
		return
	}
	note := ""
	if store, err := core.Tasks(); err == nil && store.Paused(ctx.GetNetwork()) {
		note = " (background work is paused right now; it'll start when it resumes)"
	}
	ctx.Reply(fmt.Sprintf(queued, task.ID) + note)
}

// ownedTask looks up the task an id argument names and checks ctx may manage it.
func ownedTask(ctx irc.ChatContextInterface, idArg string) (*core.TaskStore, core.Task, bool) {
	store, err := core.Tasks()
	if err != nil {
		ctx.Reply("background work is unavailable")
		return nil, core.Task{}, false
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(idArg, "#"), 10, 64)
	task, ok := store.Get(ctx.GetNetwork(), id)
	if err != nil || !ok {
		ctx.Reply("no such task")
		return nil, core.Task{}, false
	}
	if !ctx.IsAdmin() && !strings.EqualFold(task.Owner, ctx.GetSource()) {
		ctx.Reply("only the person who started it, or an admin, can do that")
		return nil, core.Task{}, false
	}
	return store, task, true
}

func cancelWork(ctx irc.ChatContextInterface, idArg string) {
	store, task, ok := ownedTask(ctx, idArg)
	if !ok {
		return
	}
	if !store.Cancel(ctx.GetNetwork(), task.ID) {
		ctx.Reply(fmt.Sprintf("#%d already finished (%s)", task.ID, task.Status))
		return
	}
	llm.CancelTask(task.ID)
	ctx.GetLogger().Info("task_cancel_requested", "task", task.ID, "by", ctx.GetSource())
	ctx.Reply(fmt.Sprintf("#%d cancelled", task.ID))
}

func argAt(args []string, i int) string {
	if i < len(args) {
		return args[i]
	}
	return ""
}

// TaskCommand starts and manages one-shot background tasks.
type TaskCommand struct{}

func (c *TaskCommand) Name() string    { return "+task" }
func (c *TaskCommand) AdminOnly() bool { return false }

const taskUsage = "usage: +task <what to do> | +task <id> | +task list | +task cancel <id> | +task result <id> | +task pause | +task resume"

func (c *TaskCommand) Execute(ctx irc.ChatContextInterface) {
	args := ctx.GetArgs()
	if len(args) < 2 {
		ctx.Reply(taskUsage)
		return
	}
	switch strings.ToLower(args[1]) {
	case "cancel":
		cancelWork(ctx, argAt(args, 2))
	case "result", "status":
		showTaskResult(ctx, argAt(args, 2))
	case "list":
		(&TasksCommand{}).Execute(ctx)
	case "pause", "resume":
		pauseWork(ctx, strings.ToLower(args[1]) == "pause")
	default:
		if len(args) == 2 && isTaskID(args[1]) {
			showTaskResult(ctx, args[1])
			return
		}
		startWork(ctx, core.TaskSpec{Kind: core.KindTask, Objective: strings.Join(args[1:], " ")},
			"queued as task #%d - i'll post the result here when it's done")
	}
}

func isTaskID(s string) bool {
	_, err := strconv.ParseInt(strings.TrimPrefix(s, "#"), 10, 64)
	return err == nil
}

func pauseWork(ctx irc.ChatContextInterface, pause bool) {
	if !ctx.IsAdmin() {
		ctx.Reply("You don't have permission to perform this action.")
		return
	}
	store, err := core.Tasks()
	if err != nil {
		ctx.Reply("background work is unavailable")
		return
	}
	if pause {
		store.Pause(ctx.GetNetwork())
		ctx.Reply("background work paused: what's running finishes, nothing new starts")
	} else {
		store.Resume(ctx.GetNetwork())
		ctx.Reply("background work resumed")
	}
	ctx.GetLogger().Info("tasks_paused", "paused", pause, "by", ctx.GetSource())
}

func showTaskResult(ctx irc.ChatContextInterface, idArg string) {
	store, err := core.Tasks()
	if err != nil {
		ctx.Reply("background work is unavailable")
		return
	}
	id, _ := strconv.ParseInt(strings.TrimPrefix(idArg, "#"), 10, 64)
	task, ok := store.Get(ctx.GetNetwork(), id)
	if !ok {
		ctx.Reply("no such task")
		return
	}
	if strings.TrimSpace(task.Result) == "" {
		ctx.Reply(fmt.Sprintf("#%d is %s, with no result yet", task.ID, task.Status))
		return
	}
	if url := llm.PasteText(ctx.GetSystem(), task.Result, ctx.GetLogger()); url != "" {
		ctx.Reply(fmt.Sprintf("#%d (%s): %s", task.ID, task.Status, url))
		return
	}
	lines := strings.Split(task.Result, "\n")
	if len(lines) > 6 {
		lines = append(lines[:6], fmt.Sprintf("... (%d more lines)", len(lines)-6))
	}
	for _, l := range lines {
		ctx.Reply(l)
	}
}

// GoalCommand starts and steers goals.
type GoalCommand struct{}

func (c *GoalCommand) Name() string    { return "+goal" }
func (c *GoalCommand) AdminOnly() bool { return false }

const goalUsage = "usage: +goal <objective> [done when: <criteria>] | +goal status|accept|cancel <id> | +goal nudge <id> <hint>"

func (c *GoalCommand) Execute(ctx irc.ChatContextInterface) {
	args := ctx.GetArgs()
	if len(args) < 2 {
		ctx.Reply(goalUsage)
		return
	}
	switch strings.ToLower(args[1]) {
	case "status":
		goalStatus(ctx, argAt(args, 2))
	case "cancel":
		cancelWork(ctx, argAt(args, 2))
	case "accept":
		store, task, ok := ownedTask(ctx, argAt(args, 2))
		if !ok {
			return
		}
		if !store.Accept(ctx.GetNetwork(), task.ID) {
			ctx.Reply(fmt.Sprintf("goal #%d isn't waiting to be accepted (%s)", task.ID, task.Status))
			return
		}
		ctx.Reply(fmt.Sprintf("goal #%d accepted - it starts shortly", task.ID))
	case "nudge":
		store, task, ok := ownedTask(ctx, argAt(args, 2))
		if !ok {
			return
		}
		hint := strings.TrimSpace(strings.Join(args[min(3, len(args)):], " "))
		if hint == "" || task.Kind != core.KindGoal {
			ctx.Reply("usage: +goal nudge <id> <hint for its next round>")
			return
		}
		_ = store.SetFeedback(task.ID, "from "+ctx.GetSource()+": "+irc.SanitizeUserMessage(hint), true)
		ctx.Reply(fmt.Sprintf("goal #%d will see that next round", task.ID))
	default:
		objective, criteria := splitCriteria(strings.Join(args[1:], " "))
		startWork(ctx, irc.GoalSpec(ctx.GetConfig(), objective, criteria),
			"goal #%d queued - i'll work on it in rounds and post when it's done")
	}
}

// splitCriteria separates "objective done when: criteria".
func splitCriteria(text string) (string, string) {
	lower := strings.ToLower(text)
	if i := strings.Index(lower, "done when:"); i >= 0 {
		return strings.TrimSpace(text[:i]), strings.TrimSpace(text[i+len("done when:"):])
	}
	return strings.TrimSpace(text), ""
}

func goalStatus(ctx irc.ChatContextInterface, idArg string) {
	store, err := core.Tasks()
	if err != nil {
		ctx.Reply("background work is unavailable")
		return
	}
	id, _ := strconv.ParseInt(strings.TrimPrefix(idArg, "#"), 10, 64)
	t, ok := store.Get(ctx.GetNetwork(), id)
	if !ok {
		ctx.Reply("no such goal")
		return
	}
	ctx.Reply(fmt.Sprintf("goal #%d %s: round %d/%d, %s spent. %s", t.ID, t.Status, t.Runs, t.MaxRuns,
		t.Spent.Round(time.Second), truncateText(t.Objective, 120)))
	if t.Criteria != "" {
		ctx.Reply("done when: " + truncateText(t.Criteria, 200))
	}
	for _, line := range strings.Split(irc.FormatTodo(t.Todo), "\n") {
		ctx.Reply(line)
	}
	if t.Feedback != "" {
		ctx.Reply("last review: " + truncateText(t.Feedback, 200))
	}
}

func truncateText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// ScheduleCommand runs something later, once or on repeat.
type ScheduleCommand struct{}

func (c *ScheduleCommand) Name() string    { return "+schedule" }
func (c *ScheduleCommand) AdminOnly() bool { return false }

const scheduleUsage = "usage: +schedule in <30m|2h|1d> <what to do> | +schedule every <2h> <...> | +schedule daily <HH:MM UTC> <...> | +schedule cancel <id>"

func (c *ScheduleCommand) Execute(ctx irc.ChatContextInterface) {
	args := ctx.GetArgs()
	if len(args) < 2 {
		ctx.Reply(scheduleUsage)
		return
	}
	if strings.ToLower(args[1]) == "cancel" {
		cancelWork(ctx, argAt(args, 2))
		return
	}
	if len(args) < 4 {
		ctx.Reply(scheduleUsage)
		return
	}
	spec, err := parseSchedule(strings.ToLower(args[1]), args[2], time.Now())
	if err != nil {
		ctx.Reply(err.Error() + " - " + scheduleUsage)
		return
	}
	spec.Objective = strings.Join(args[3:], " ")
	when := spec.NextRun.UTC().Format("Jan 2 15:04 UTC")
	if spec.Interval > 0 {
		startWork(ctx, spec, "scheduled as #%d: first run "+when+", then every "+spec.Interval.String())
		return
	}
	startWork(ctx, spec, "scheduled as #%d for "+when)
}

// parseSchedule reads "in <duration>", "every <duration>" or "daily <HH:MM>" (UTC).
func parseSchedule(mode, arg string, now time.Time) (core.TaskSpec, error) {
	spec := core.TaskSpec{Kind: core.KindSchedule}
	switch mode {
	case "in", "every":
		d, err := parseDuration(arg)
		if err != nil || d < time.Minute || d > 30*24*time.Hour {
			return spec, fmt.Errorf("can't read %q as a time between 1m and 30d", arg)
		}
		spec.NextRun = now.Add(d)
		if mode == "every" {
			spec.Interval = d
		}
	case "daily":
		at, err := time.Parse("15:04", arg)
		if err != nil {
			return spec, fmt.Errorf("can't read %q as HH:MM", arg)
		}
		n := now.UTC()
		next := time.Date(n.Year(), n.Month(), n.Day(), at.Hour(), at.Minute(), 0, 0, time.UTC)
		if !next.After(n) {
			next = next.Add(24 * time.Hour)
		}
		spec.NextRun, spec.Interval = next, 24*time.Hour
	default:
		return spec, fmt.Errorf("say in, every or daily")
	}
	return spec, nil
}

// parseDuration accepts Go durations plus a "d" suffix for days.
func parseDuration(s string) (time.Duration, error) {
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil {
			return 0, err
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

// TasksCommand lists background work on this network.
type TasksCommand struct{}

func (c *TasksCommand) Name() string    { return "+tasks" }
func (c *TasksCommand) AdminOnly() bool { return false }

func (c *TasksCommand) Execute(ctx irc.ChatContextInterface) {
	store, err := core.Tasks()
	if err != nil {
		ctx.Reply("background work is unavailable")
		return
	}
	tasks, err := store.List(ctx.GetNetwork(), 5)
	if err != nil {
		ctx.GetLogger().Error("tasks_list_failed", "error", err.Error())
		ctx.Reply("couldn't list background work")
		return
	}
	if len(tasks) == 0 {
		ctx.Reply("no background work")
		return
	}
	if store.Paused(ctx.GetNetwork()) {
		ctx.Reply("(background work is paused)")
	}
	for _, t := range tasks {
		ctx.Reply(fmt.Sprintf("#%d %s %s%s (%s): %s", t.ID, t.Kind, t.Status, taskAge(t), t.Owner, truncateText(t.Objective, 70)))
	}
}

func taskAge(t core.Task) string {
	switch {
	case t.Status == core.TaskRunning:
		return " for " + time.Since(t.Started).Round(time.Second).String()
	case t.Status == core.TaskQueued && t.NextRun.After(time.Now()):
		return " until " + t.NextRun.UTC().Format("Jan 2 15:04 UTC")
	case t.Kind == core.KindGoal && t.Runs > 0:
		return fmt.Sprintf(" round %d/%d", t.Runs, t.MaxRuns)
	}
	return ""
}
