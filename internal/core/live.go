// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// LiveEvent is one log record or one piece of a model's reasoning, for the operator page. Kept in
// memory only; nothing here is written anywhere.
type LiveEvent struct {
	Seq     uint64            `json:"seq"`
	Time    time.Time         `json:"time"`
	Level   string            `json:"level,omitempty"`
	Message string            `json:"msg,omitempty"`
	Attrs   map[string]string `json:"attrs,omitempty"`
	// Thinking: which request it belongs to, who asked, where, and the next piece of text.
	Request string `json:"request,omitempty"`
	Network string `json:"network,omitempty"`
	Source  string `json:"source,omitempty"`
	Target  string `json:"target,omitempty"`
	Text    string `json:"text,omitempty"`
}

// LiveHub keeps the latest events in a ring and hands new ones to every subscriber. A subscriber that
// can't keep up misses events rather than holding anything up.
type LiveHub struct {
	mu   sync.Mutex
	ring []LiveEvent
	next int
	full bool
	seq  uint64
	subs map[chan LiveEvent]struct{}
}

// NewLiveHub keeps the last size events.
func NewLiveHub(size int) *LiveHub {
	return &LiveHub{ring: make([]LiveEvent, size), subs: map[chan LiveEvent]struct{}{}}
}

// Publish stamps and records e, and passes it on.
func (h *LiveHub) Publish(e LiveEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq++
	e.Seq = h.seq
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	h.ring[h.next] = e
	h.next = (h.next + 1) % len(h.ring)
	h.full = h.full || h.next == 0
	for ch := range h.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

// Subscribe returns what the ring holds, oldest first, then delivers new events until cancel.
func (h *LiveHub) Subscribe() (backlog []LiveEvent, events <-chan LiveEvent, cancel func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.full {
		backlog = append(backlog, h.ring[h.next:]...)
	}
	backlog = append(backlog, h.ring[:h.next]...)
	ch := make(chan LiveEvent, 256)
	h.subs[ch] = struct{}{}
	return backlog, ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}
}

// LiveLogs and LiveThinking feed the operator page. Thinking arrives in many small pieces, so it gets
// its own ring and can't push the logs out.
var (
	LiveLogs     = NewLiveHub(2000)
	LiveThinking = NewLiveHub(4000)
)

// teeHandler passes every record to the real handler and a copy to LiveLogs.
type teeHandler struct {
	inner slog.Handler
	attrs []slog.Attr
}

func (t teeHandler) Enabled(ctx context.Context, l slog.Level) bool { return t.inner.Enabled(ctx, l) }

func (t teeHandler) Handle(ctx context.Context, r slog.Record) error {
	// Reasoning reaches the page as thinking; the per-chunk debug line would only crowd the logs.
	if r.Message != "reasoning_chunk" {
		attrs := make(map[string]string, len(t.attrs)+r.NumAttrs())
		for _, a := range t.attrs {
			attrs[a.Key] = a.Value.String()
		}
		r.Attrs(func(a slog.Attr) bool {
			attrs[a.Key] = a.Value.String()
			return true
		})
		LiveLogs.Publish(LiveEvent{Time: r.Time, Level: r.Level.String(), Message: r.Message, Attrs: attrs})
	}
	return t.inner.Handle(ctx, r)
}

func (t teeHandler) WithAttrs(as []slog.Attr) slog.Handler {
	return teeHandler{inner: t.inner.WithAttrs(as), attrs: append(append([]slog.Attr{}, t.attrs...), as...)}
}

// Groups are flattened: the page shows keys, and nothing here logs in groups.
func (t teeHandler) WithGroup(name string) slog.Handler {
	return teeHandler{inner: t.inner.WithGroup(name), attrs: t.attrs}
}
