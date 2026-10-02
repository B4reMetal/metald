// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/alexschlessinger/pollytool/messages"
	"github.com/lrstanley/girc"

	"B4reMetal/metald/internal/config"
	"B4reMetal/metald/internal/core"
	"B4reMetal/metald/internal/irc"
)

// Background work runs here, one round at a time per network: a task's only run, a goal's next
// round, or a schedule that has come due. Each round is an ordinary agent run in the work's own
// conversation, with a larger budget than a chat request; what happens after it depends on the kind.

const (
	taskPollInterval = 5 * time.Second
	// taskInlineLines is how much of a result goes straight to the channel; the rest is pasted.
	taskInlineLines = 6
	// taskInlineLinks is how many lines with links past the first few still go to the channel.
	taskInlineLinks = 3
	// taskPasteTool publishes results too long for the channel.
	taskPasteTool = "paste__post_gist"
	// proposalTTL is how long a proposed goal waits to be accepted.
	proposalTTL = time.Hour
)

// runningTasks maps a running task's id to the cancel func of its context.
var runningTasks sync.Map

// lastProgress maps a goal's id to when it last posted a progress line.
var lastProgress sync.Map

// CancelTask stops a running task, if it is running here, and reports whether it was.
func CancelTask(id int64) bool {
	v, ok := runningTasks.Load(id)
	if ok {
		v.(context.CancelFunc)()
	}
	return ok
}

func taskSessionKey(t core.Task) string {
	return fmt.Sprintf("%s%s/%d", irc.TaskSessionPrefix, t.Network, t.ID)
}

// taskConfig is cfg with a round's budget: its own time, iteration and output limits, and no
// "calling X" announcements, which would interrupt the channel's conversation. The larger output
// limit matters for code: the sandbox starts fresh every call, so a whole file must be written in
// one tool call, after the thinking that produced it.
func taskConfig(cfg *config.Configuration, timeout time.Duration) *config.Configuration {
	c := *cfg
	api := *cfg.API
	api.Timeout = timeout
	c.API = &api
	model := *cfg.Model
	if cfg.Session.TaskMaxTokens > model.MaxTokens {
		model.MaxTokens = cfg.Session.TaskMaxTokens
	}
	c.Model = &model
	session := *cfg.Session
	session.MaxIterations = cfg.Session.TaskMaxIterations
	c.Session = &session
	bot := *cfg.Bot
	bot.ShowToolActions = false
	bot.ShowThinkingAction = false
	c.Bot = &bot
	return &c
}

// roundTimeout is how long the next round may take: a task's full budget, or what is left of a goal's.
func roundTimeout(cfg *config.Configuration, task core.Task) time.Duration {
	limit := cfg.Session.TaskMaxTime
	if task.Kind == core.KindGoal {
		if left := cfg.Session.GoalMaxTime - task.Spent; left < limit {
			limit = left
		}
	}
	return max(limit, time.Minute)
}

// RunTaskRunner works through a network's background work until ctx ends. Work a restart
// interrupted is queued again first and resumes its saved conversation.
func RunTaskRunner(ctx context.Context, cfg *config.Configuration, sys core.System, client *girc.Client, fatalCh chan<- error) {
	store, err := core.Tasks()
	if err != nil {
		slog.Error("tasks_unavailable", "error", err.Error())
		return
	}
	network := cfg.Server.Name
	if n := store.Requeue(network); n > 0 {
		slog.Info("tasks_requeued", "network", network, "count", n)
	}
	t := time.NewTicker(taskPollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if n := store.ExpireProposals(network, proposalTTL); n > 0 {
			slog.Info("goal_proposals_expired", "network", network, "count", n)
		}
		if !client.IsConnected() {
			continue
		}
		if task, ok := store.Claim(network); ok {
			runRound(ctx, cfg, sys, client, fatalCh, store, task)
		}
	}
}

// round is what one run of background work produced.
type round struct {
	lines    []string
	evidence string // the round's own messages, for a goal's reviewer
	ran      time.Duration
	timedOut bool
}

func runRound(parent context.Context, cfg *config.Configuration, sys core.System, client *girc.Client,
	fatalCh chan<- error, store *core.TaskStore, task core.Task) {
	log := slog.With("task", task.ID, "kind", task.Kind, "network", task.Network, "channel", task.Channel, "owner", task.Owner)
	nick := task.Owner
	if src := girc.ParseSource(task.OwnerMask); src != nil && src.Name != "" {
		nick = src.Name
	}

	tctx, cancel, err := irc.NewTaskContext(parent, taskConfig(cfg, roundTimeout(cfg, task)), sys, client,
		task.Channel, task.OwnerMask, task.Objective, taskSessionKey(task), fatalCh)
	if err != nil {
		log.Error("task_context_failed", "error", err.Error())
		store.Finish(task.ID, core.TaskFailed, "could not start", 0)
		return
	}
	runningTasks.Store(task.ID, cancel)
	defer runningTasks.Delete(task.ID)
	defer cancel()

	resumed := prepareTaskSession(tctx, cfg)
	msg := roundMessage(cfg, task, resumed)
	log.Info("task_started", "round", task.Runs+1, "resumed", resumed, "objective", truncate(task.Objective, 200))

	start := time.Now()
	var r round
	core.WithRequestLock(tctx, fmt.Sprintf("task:%d", task.ID), "task", func() {
		out, err := Complete(tctx, fmt.Sprintf("(nick:%s) %s", nick, msg))
		if err != nil {
			log.Error("task_completion_error", "error", err.Error())
			return
		}
		for line := range out {
			if strings.TrimSpace(line) != "" {
				r.lines = append(r.lines, line)
			}
		}
	}, nil)
	r.ran = time.Since(start)
	r.timedOut = errors.Is(tctx.Err(), context.DeadlineExceeded)
	r.evidence = roundEvidence(tctx.GetSession().GetHistory())

	if current, ok := store.Get(task.Network, task.ID); ok && current.Status == core.TaskCancelled {
		log.Info("task_cancelled", "duration_ms", r.ran.Milliseconds())
		return
	}
	switch task.Kind {
	case core.KindGoal:
		endGoalRound(tctx, cfg, store, task, nick, r, log)
	case core.KindSchedule:
		endScheduleRun(tctx, store, task, nick, r, log)
	default:
		endTask(tctx, store, task, nick, r, log)
	}
}

// roundMessage is what a round opens with, by kind.
func roundMessage(cfg *config.Configuration, task core.Task, resumed bool) string {
	objective := irc.SanitizeUserMessage(task.Objective)
	switch {
	case task.Kind == core.KindGoal:
		return goalMessage(cfg, task, !resumed)
	case resumed:
		return "[this was interrupted by a restart - continue from where it stopped] " + objective
	case task.Kind == core.KindSchedule:
		return "[scheduled for now] " + objective
	}
	return objective
}

func endTask(ctx irc.ChatContextInterface, store *core.TaskStore, task core.Task, nick string, r round, log *slog.Logger) {
	status, result := outcome(r)
	store.Finish(task.ID, status, result, r.ran)
	log.Info("task_finished", "status", status, "duration_ms", r.ran.Milliseconds(), "lines", len(r.lines))
	head := fmt.Sprintf("%s: task #%d is done:", nick, task.ID)
	if status != core.TaskDone {
		head = fmt.Sprintf("%s: task #%d ran out of time; here's what it got:", nick, task.ID)
	}
	postResult(ctx, task, nick, head, r.lines)
}

func endScheduleRun(ctx irc.ChatContextInterface, store *core.TaskStore, task core.Task, nick string, r round, log *slog.Logger) {
	status, result := outcome(r)
	if task.Interval > 0 {
		next := task.NextRun.Add(task.Interval)
		for !next.After(time.Now()) {
			next = next.Add(task.Interval)
		}
		store.Reschedule(task.ID, next, result, r.ran)
		log.Info("schedule_ran", "status", status, "next", next.UTC().Format(time.RFC3339))
	} else {
		store.Finish(task.ID, status, result, r.ran)
		log.Info("task_finished", "status", status, "duration_ms", r.ran.Milliseconds(), "lines", len(r.lines))
	}
	postResult(ctx, task, nick, fmt.Sprintf("%s: scheduled #%d:", nick, task.ID), r.lines)
}

func endGoalRound(ctx irc.ChatContextInterface, cfg *config.Configuration, store *core.TaskStore, task core.Task,
	nick string, r round, log *slog.Logger) {
	result := strings.Join(r.lines, "\n")
	verdict := Verdict{Kind: "continue", Reason: "the last round ran out of time; work in smaller steps and save progress with task__note"}
	if !r.timedOut {
		verdict = verifyGoal(cfg, task, r.evidence)
	}
	roundNo := task.Runs + 1
	log.Info("goal_reviewed", "round", roundNo, "verdict", verdict.Kind, "reason", truncate(verdict.Reason, 200))

	outOfBudget := ""
	switch {
	case roundNo >= task.MaxRuns:
		outOfBudget = fmt.Sprintf("used all %d rounds", task.MaxRuns)
	case task.Spent+r.ran >= cfg.Session.GoalMaxTime:
		outOfBudget = "used its time budget"
	case !task.Deadline.IsZero() && time.Now().After(task.Deadline):
		outOfBudget = "passed its deadline"
	}

	switch {
	case verdict.Kind == "done":
		store.Finish(task.ID, core.TaskDone, result, r.ran)
		lastProgress.Delete(task.ID)
		postResult(ctx, task, nick, fmt.Sprintf("%s: goal #%d is done after %d round(s) - %s", nick, task.ID, roundNo,
			truncate(orNone(verdict.Reason), 140)), r.lines)
	case verdict.Kind == "stuck" || outOfBudget != "":
		why := verdict.Reason
		if outOfBudget != "" {
			why = outOfBudget + "; still missing: " + orNone(verdict.Reason)
		}
		store.Finish(task.ID, core.TaskFailed, result+"\n(stopped: "+why+")", r.ran)
		lastProgress.Delete(task.ID)
		postResult(ctx, task, nick, fmt.Sprintf("%s: goal #%d stopped after %d round(s) - %s. what it got:", nick,
			task.ID, roundNo, why), r.lines)
	default:
		_ = store.SetFeedback(task.ID, verdict.Reason, false)
		store.Reschedule(task.ID, time.Now().Add(goalRoundGap), result, r.ran)
		postProgress(ctx, cfg, task, roundNo, verdict.Reason)
	}
}

// postProgress tells the channel a goal is still going, at most once per goalupdateinterval.
func postProgress(ctx irc.ChatContextInterface, cfg *config.Configuration, task core.Task, roundNo int, next string) {
	if v, ok := lastProgress.Load(task.ID); ok && time.Since(v.(time.Time)) < cfg.Session.GoalUpdateInterval {
		return
	}
	lastProgress.Store(task.ID, time.Now())
	ctx.Reply(fmt.Sprintf("goal #%d, round %d/%d: not there yet - %s", task.ID, roundNo, task.MaxRuns, truncate(orNone(next), 160)))
}

// outcome is a single run's status and the text kept as its result.
func outcome(r round) (string, string) {
	result := strings.Join(r.lines, "\n")
	switch {
	case r.timedOut:
		return core.TaskFailed, strings.TrimSpace(result + "\n(ran out of time)")
	case len(r.lines) == 0:
		return core.TaskFailed, result
	}
	return core.TaskDone, result
}

// prepareTaskSession gives the work's conversation the task prompt, keeping any work it already did,
// and reports whether there was earlier work to resume.
func prepareTaskSession(ctx irc.ChatContextInterface, cfg *config.Configuration) bool {
	session := ctx.GetSession()
	want := strings.TrimSpace(cfg.Bot.Prompt) + "\n\n" + strings.TrimSpace(cfg.Bot.TaskPrompt)
	_, convo := splitSystem(session.GetHistory())
	md := session.GetMetadata()
	if md.SystemPrompt != want {
		md.SystemPrompt = want
		session.SetMetadata(md)
		mu := core.CommitLock(session)
		mu.Lock()
		session.Clear()
		for _, m := range convo {
			session.AddMessage(m)
		}
		mu.Unlock()
	}
	for _, m := range convo {
		if m.Role == messages.MessageRoleUser {
			return true
		}
	}
	return false
}

// postResult reports finished work where it was asked for: the prose inline, the whole result as a
// paste when there is more.
func postResult(ctx irc.ChatContextInterface, task core.Task, nick, head string, lines []string) {
	if len(lines) == 0 {
		ctx.Reply(fmt.Sprintf("%s: #%d came to nothing - try asking for less at once.", nick, task.ID))
		return
	}
	ctx.Reply(head)
	shown, cut := inlineResult(lines)
	for _, l := range shown {
		ctx.Reply(l)
	}
	if cut {
		if url := PasteText(ctx.GetSystem(), strings.Join(lines, "\n"), ctx.GetLogger()); url != "" {
			ctx.Reply(fmt.Sprintf("full result (%d lines): %s", len(lines), url))
		} else {
			ctx.Reply(fmt.Sprintf("(more in +task result %d)", task.ID))
		}
	}
}

// taskEcho matches the "(nick:x)" identity prefix a model sometimes copies onto its own answer.
var taskEcho = regexp.MustCompile(`^\(nick:[^)]*\)\s*`)

// inlineResult picks what of a result goes straight to the channel: its prose, without code blocks,
// up to a few lines, plus any later line with a link - the link is usually the deliverable. cut
// reports whether anything was left out, so the whole result gets pasted.
func inlineResult(lines []string) (shown []string, cut bool) {
	inCode := false
	links := 0
	for i, l := range lines {
		if i == 0 {
			l = taskEcho.ReplaceAllString(l, "")
		}
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			inCode = !inCode
			cut = true
			continue
		}
		if inCode || strings.TrimSpace(l) == "" {
			cut = cut || inCode
			continue
		}
		if len(shown) >= taskInlineLines {
			cut = true
			if !strings.Contains(l, "://") || links == taskInlineLinks {
				continue
			}
			links++
		}
		shown = append(shown, l)
	}
	return shown, cut
}

// PasteText publishes text with the paste tool and returns its url, or "" if that is not possible.
func PasteText(sys core.System, text string, log *slog.Logger) string {
	if sys == nil || sys.GetToolRegistry() == nil {
		return ""
	}
	tool, ok := sys.GetToolRegistry().Get(taskPasteTool)
	if !ok {
		return ""
	}
	pctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := tool.Execute(pctx, map[string]any{"content": text, "language": "text"})
	if err != nil {
		log.Warn("paste_failed", "error", err.Error())
		return ""
	}
	for _, line := range strings.Split(out, "\n") {
		if u, ok := strings.CutPrefix(strings.TrimSpace(line), "url: "); ok {
			return strings.TrimSpace(u)
		}
	}
	return ""
}
