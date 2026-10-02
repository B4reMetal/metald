// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package admin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"B4reMetal/metald/internal/core"
)

const recentTasks = 40

func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, msg string) {
	respond(w, status, map[string]string{"error": msg})
}

func unix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// network is the {network} in the path, if the bot runs on it.
func (s *Server) network(w http.ResponseWriter, r *http.Request) (string, bool) {
	n := r.PathValue("network")
	if !slices.Contains(s.cfg.Networks, n) {
		fail(w, http.StatusNotFound, "no such network")
		return "", false
	}
	return n, true
}

func (s *Server) whoami(w http.ResponseWriter, r *http.Request) {
	if user := s.proxyUser(r); user != "" {
		respond(w, http.StatusOK, map[string]string{"user": user, "via": "proxy"})
		return
	}
	respond(w, http.StatusOK, map[string]string{"user": "", "via": "token"})
}

func (s *Server) status(w http.ResponseWriter, _ *http.Request) {
	nets := make([]networkState, 0, len(s.cfg.Networks))
	for _, n := range s.cfg.Networks {
		nets = append(nets, networkState{Name: n, Paused: s.tasks.Paused(n)})
	}
	inflight := []inflightRequest{}
	for _, f := range s.work.Inflight() {
		inflight = append(inflight, inflightRequest{Key: f.Key, Operation: f.Operation, Source: f.Source,
			Since: f.Since.Unix(), Running: f.Running})
	}
	respond(w, http.StatusOK, statusResponse{Version: s.cfg.Version, Started: s.cfg.Started.Unix(),
		Now: time.Now().Unix(), Concurrency: s.work.Concurrency(), Networks: nets, Inflight: inflight})
}

func (s *Server) listTasks(w http.ResponseWriter, _ *http.Request) {
	out := []taskView{}
	for _, n := range s.cfg.Networks {
		list, err := s.tasks.List(n, recentTasks)
		if err != nil {
			fail(w, http.StatusInternalServerError, "listing tasks failed")
			return
		}
		for _, t := range list {
			out = append(out, taskView{
				ID: t.ID, Network: n, Kind: t.Kind, Status: t.Status, Channel: t.Channel, Owner: t.Owner,
				Objective: t.Objective, Result: clip(t.Result, 2000), Runs: t.Runs, MaxRuns: t.MaxRuns,
				Interval: int64(t.Interval.Seconds()), Created: unix(t.Created), NextRun: unix(t.NextRun),
				Finished: unix(t.Finished),
				Active:   slices.Contains([]string{core.TaskProposed, core.TaskQueued, core.TaskRunning}, t.Status),
			})
		}
	}
	respond(w, http.StatusOK, map[string]any{"tasks": out})
}

// cancelTask does what +task cancel does: mark it cancelled, then stop a round already running.
func (s *Server) cancelTask(w http.ResponseWriter, r *http.Request) {
	n, ok := s.network(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, http.StatusBadRequest, "task ids are numbers")
		return
	}
	if !s.tasks.Cancel(n, id) {
		fail(w, http.StatusConflict, "no such task, or it already finished")
		return
	}
	s.work.StopRound(id)
	respond(w, http.StatusOK, map[string]any{"cancelled": id})
}

func (s *Server) setPaused(w http.ResponseWriter, r *http.Request) {
	n, ok := s.network(w, r)
	if !ok {
		return
	}
	var in struct {
		Paused *bool `json:"paused"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&in); err != nil || in.Paused == nil {
		fail(w, http.StatusBadRequest, `send {"paused": true} or {"paused": false}`)
		return
	}
	if *in.Paused {
		s.tasks.Pause(n)
	} else {
		s.tasks.Resume(n)
	}
	respond(w, http.StatusOK, networkState{Name: n, Paused: *in.Paused})
}

func (s *Server) listReminders(w http.ResponseWriter, _ *http.Request) {
	out := []reminderView{}
	for _, n := range s.cfg.Networks {
		for _, rm := range s.reminders.List(n) {
			out = append(out, reminderView{ID: rm.ID, Network: n, Nick: rm.Nick, Channel: rm.Channel,
				Text: rm.Text, Due: rm.Due.Unix(), SetBy: rm.SetBy})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Due < out[j].Due })
	respond(w, http.StatusOK, map[string]any{"reminders": out})
}

func (s *Server) cancelReminder(w http.ResponseWriter, r *http.Request) {
	n, ok := s.network(w, r)
	if !ok {
		return
	}
	if !s.reminders.Cancel(n, r.PathValue("id")) {
		fail(w, http.StatusNotFound, "no such reminder")
		return
	}
	respond(w, http.StatusOK, map[string]any{"cancelled": r.PathValue("id")})
}

func (s *Server) radio(w http.ResponseWriter, _ *http.Request) {
	out := radioResponse{Configured: s.cfg.RadioURL != "", Queue: []string{}}
	if out.Configured {
		base := strings.TrimRight(s.cfg.RadioURL, "/")
		var now struct {
			Title     string `json:"title"`
			By        string `json:"by"`
			Listeners *int   `json:"listeners"`
		}
		var queue struct {
			Queue []string `json:"queue"`
		}
		if s.getJSON(base+"/now", &now) == nil && s.getJSON(base+"/queue", &queue) == nil {
			out.Reachable, out.Title, out.By, out.Listeners = true, now.Title, now.By, now.Listeners
			if queue.Queue != nil {
				out.Queue = queue.Queue
			}
		}
	}
	respond(w, http.StatusOK, out)
}

func (s *Server) getJSON(url string, v any) error {
	resp, err := s.client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New(resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(v)
}
