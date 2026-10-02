// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// comfyJob is one entry of ComfyUI's /queue: [number, prompt id, workflow, extra data, outputs].
type comfyJob []json.RawMessage

type comfyNode struct {
	Class  string         `json:"class_type"`
	Inputs map[string]any `json:"inputs"`
}

// firstSeen remembers when each job id first showed up, for how long it has waited or run.
type firstSeen struct {
	mu   sync.Mutex
	when map[string]time.Time
}

func (f *firstSeen) mark(ids []string) map[string]time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.when == nil {
		f.when = map[string]time.Time{}
	}
	now, live := time.Now(), map[string]bool{}
	for _, id := range ids {
		live[id] = true
		if _, ok := f.when[id]; !ok {
			f.when[id] = now
		}
	}
	for id := range f.when {
		if !live[id] {
			delete(f.when, id)
		}
	}
	out := make(map[string]time.Time, len(f.when))
	for k, v := range f.when {
		out[k] = v
	}
	return out
}

func (s *Server) comfyQueue() (running, pending []comfyJob, err error) {
	var q struct {
		Running []comfyJob `json:"queue_running"`
		Pending []comfyJob `json:"queue_pending"`
	}
	err = s.getJSON(strings.TrimRight(s.cfg.ComfyURL, "/")+"/queue", &q)
	// ComfyUI lists waiting jobs in no particular order; their number is their place in line.
	sort.SliceStable(q.Pending, func(i, j int) bool { return jobNumber(q.Pending[i]) < jobNumber(q.Pending[j]) })
	return q.Running, q.Pending, err
}

func jobNumber(j comfyJob) float64 {
	var n float64
	if len(j) > 0 {
		_ = json.Unmarshal(j[0], &n)
	}
	return n
}

func jobID(j comfyJob) string {
	var id string
	if len(j) > 1 {
		_ = json.Unmarshal(j[1], &id)
	}
	return id
}

// describe says what a job is: its kind from the workflow's node types, which tool sent it (comfyui.py
// tags it), and the words that went in (a song's style, an image prompt, the text to speak).
func describe(j comfyJob) (kind, tool, summary string) {
	var nodes map[string]comfyNode
	if len(j) > 2 {
		_ = json.Unmarshal(j[2], &nodes)
	}
	var extra struct {
		Metald struct {
			Tool string `json:"tool"`
		} `json:"metald"`
	}
	if len(j) > 3 {
		_ = json.Unmarshal(j[3], &extra)
	}
	var classes []string
	var texts []string
	for _, n := range nodes {
		classes = append(classes, strings.ToLower(n.Class))
		for _, key := range []string{"tags", "text", "prompt", "positive"} {
			if v, ok := n.Inputs[key].(string); ok && strings.TrimSpace(v) != "" {
				texts = append(texts, strings.Join(strings.Fields(v), " "))
			}
		}
	}
	sort.Slice(texts, func(a, b int) bool { return len(texts[a]) > len(texts[b]) })
	if len(texts) > 0 {
		summary = clip(texts[0], 160)
	}
	return jobKind(strings.Join(classes, " ")), extra.Metald.Tool, summary
}

func jobKind(classes string) string {
	switch {
	case strings.Contains(classes, "acestep"):
		return "song"
	case strings.Contains(classes, "chatterbox"):
		return "speech"
	case strings.Contains(classes, "video") || strings.Contains(classes, "wan"):
		return "video"
	case strings.Contains(classes, "saveimage"):
		return "image"
	}
	return "job"
}

func (s *Server) gpu(w http.ResponseWriter, _ *http.Request) {
	out := gpuResponse{Configured: s.cfg.ComfyURL != "", Jobs: []gpuJob{}}
	if !out.Configured {
		respond(w, http.StatusOK, out)
		return
	}
	running, pending, err := s.comfyQueue()
	if err != nil {
		respond(w, http.StatusOK, out)
		return
	}
	out.Reachable = true
	var ids []string
	for _, j := range append(append([]comfyJob{}, running...), pending...) {
		ids = append(ids, jobID(j))
	}
	seen := s.seen.mark(ids)
	add := func(j comfyJob, state string, pos int) {
		kind, tool, summary := describe(j)
		out.Jobs = append(out.Jobs, gpuJob{ID: jobID(j), State: state, Position: pos, Kind: kind, Tool: tool,
			Summary: summary, Since: seen[jobID(j)].Unix()})
	}
	for _, j := range running {
		add(j, "running", 0)
	}
	for i, j := range pending {
		add(j, "waiting", i+1)
	}
	respond(w, http.StatusOK, out)
}

// cancelGPU removes a waiting job from ComfyUI's queue, or interrupts the running one. The tool that
// sent it then reports its usual "backend unavailable" error to the channel.
func (s *Server) cancelGPU(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.cfg.ComfyURL == "" {
		fail(w, http.StatusNotFound, "no GPU backend configured")
		return
	}
	running, pending, err := s.comfyQueue()
	if err != nil {
		fail(w, http.StatusBadGateway, "the GPU backend is unreachable")
		return
	}
	base := strings.TrimRight(s.cfg.ComfyURL, "/")
	has := func(list []comfyJob) bool {
		for _, j := range list {
			if jobID(j) == id {
				return true
			}
		}
		return false
	}
	switch {
	case has(pending):
		err = s.postJSON(base+"/queue", map[string]any{"delete": []string{id}})
	case has(running):
		err = s.postJSON(base+"/interrupt", map[string]any{"prompt_id": id})
	default:
		fail(w, http.StatusNotFound, "that job is no longer queued")
		return
	}
	if err != nil {
		fail(w, http.StatusBadGateway, "the GPU backend refused: "+err.Error())
		return
	}
	respond(w, http.StatusOK, map[string]string{"cancelled": id})
}

func (s *Server) postJSON(url string, body any) error {
	b, _ := json.Marshal(body)
	resp, err := s.client.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New(resp.Status)
	}
	return nil
}
