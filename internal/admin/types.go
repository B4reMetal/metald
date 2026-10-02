// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package admin

// The API's response shapes; web/admin/src/lib/types.ts mirrors them.

type statusResponse struct {
	Version     string            `json:"version"`
	Started     int64             `json:"started"`
	Now         int64             `json:"now"`
	Concurrency int               `json:"concurrency"`
	Networks    []networkState    `json:"networks"`
	Inflight    []inflightRequest `json:"inflight"`
}

type networkState struct {
	Name   string `json:"name"`
	Paused bool   `json:"paused"`
}

type inflightRequest struct {
	Key       string `json:"key"`
	Operation string `json:"operation"`
	Source    string `json:"source"`
	Since     int64  `json:"since"`
	Running   bool   `json:"running"`
}

type taskView struct {
	ID        int64  `json:"id"`
	Network   string `json:"network"`
	Kind      string `json:"kind"`
	Status    string `json:"status"`
	Channel   string `json:"channel"`
	Owner     string `json:"owner"`
	Objective string `json:"objective"`
	Result    string `json:"result"`
	Runs      int    `json:"runs"`
	MaxRuns   int    `json:"maxRuns"`
	Interval  int64  `json:"intervalSeconds"`
	Created   int64  `json:"created"`
	NextRun   int64  `json:"nextRun"`
	Finished  int64  `json:"finished"`
	Active    bool   `json:"active"`
}

type reminderView struct {
	ID      string `json:"id"`
	Network string `json:"network"`
	Nick    string `json:"nick"`
	Channel string `json:"channel"`
	Text    string `json:"text"`
	Due     int64  `json:"due"`
	SetBy   string `json:"setBy"`
}

type gpuResponse struct {
	Configured bool     `json:"configured"`
	Reachable  bool     `json:"reachable"`
	Jobs       []gpuJob `json:"jobs"`
}

type gpuJob struct {
	ID       string `json:"id"`
	State    string `json:"state"` // "running" or "waiting"
	Position int    `json:"position"`
	Kind     string `json:"kind"`
	Tool     string `json:"tool"`
	Summary  string `json:"summary"`
	// When this page first saw it in the queue (unix seconds); ComfyUI doesn't say when it was added.
	Since int64 `json:"since"`
}

type radioResponse struct {
	Configured bool     `json:"configured"`
	Reachable  bool     `json:"reachable"`
	Title      string   `json:"title"`
	By         string   `json:"by"`
	Listeners  *int     `json:"listeners"`
	Queue      []string `json:"queue"`
}
