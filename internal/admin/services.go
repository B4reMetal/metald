// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package admin

import (
	"log/slog"

	"B4reMetal/metald/internal/core"
	"B4reMetal/metald/internal/llm"
)

// TaskService is the slice of core.TaskStore the page uses.
type TaskService interface {
	List(network string, recent int) ([]core.Task, error)
	Cancel(network string, id int64) bool
	Pause(network string)
	Resume(network string)
	Paused(network string) bool
}

// ReminderService is the slice of core.ReminderStore the page uses.
type ReminderService interface {
	List(network string) []core.Reminder
	Cancel(network, id string) bool
}

// WorkService is what is running right now, and stopping a round in progress.
type WorkService interface {
	Inflight() []core.Inflight
	Concurrency() int
	StopRound(taskID int64)
}

type coreWork struct{}

func (coreWork) Inflight() []core.Inflight { return core.InflightRequests() }
func (coreWork) Concurrency() int          { return core.Concurrency() }
func (coreWork) StopRound(id int64)        { llm.CancelTask(id) }

// NewFromCore builds the server over the bot's own stores.
func NewFromCore(cfg Config, log *slog.Logger) (*Server, error) {
	tasks, err := core.Tasks()
	if err != nil {
		return nil, err
	}
	return New(cfg, tasks, core.Reminders(), coreWork{}, Feeds{Logs: core.LiveLogs, Thinking: core.LiveThinking}, log), nil
}
