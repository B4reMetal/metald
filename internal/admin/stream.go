// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package admin

import (
	"encoding/json"
	"net/http"
	"time"

	"B4reMetal/metald/internal/core"
)

// Feed is a live source of events: the backlog first, then new ones until cancel.
type Feed interface {
	Subscribe() (backlog []core.LiveEvent, events <-chan core.LiveEvent, cancel func())
}

// Feeds are what the page can stream.
type Feeds struct{ Logs, Thinking Feed }

// stream sends a feed as Server-Sent Events. The page reads it with fetch, not EventSource, so the
// token can travel in a header like every other call.
func stream(feed Feed) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if feed == nil {
			fail(w, http.StatusNotFound, "not available")
			return
		}
		backlog, events, cancel := feed.Subscribe()
		defer cancel()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Accel-Buffering", "no")
		rc := http.NewResponseController(w)
		send := func(e core.LiveEvent) bool {
			b, _ := json.Marshal(e)
			if _, err := w.Write(append(append([]byte("data: "), b...), '\n', '\n')); err != nil {
				return false
			}
			return true
		}
		for _, e := range backlog {
			if !send(e) {
				return
			}
		}
		if rc.Flush() != nil {
			return
		}
		keepalive := time.NewTicker(15 * time.Second)
		defer keepalive.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case e := <-events:
				if !send(e) {
					return
				}
			case <-keepalive.C:
				if _, err := w.Write([]byte(": keepalive\n\n")); err != nil {
					return
				}
			}
			if rc.Flush() != nil {
				return
			}
		}
	}
}
