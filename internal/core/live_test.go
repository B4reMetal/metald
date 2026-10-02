// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"bytes"
	"log/slog"
	"testing"
)

func TestLiveHubKeepsTheLatestAndFansOut(t *testing.T) {
	h := NewLiveHub(3)
	for _, m := range []string{"a", "b", "c", "d"} {
		h.Publish(LiveEvent{Message: m})
	}
	backlog, events, cancel := h.Subscribe()
	defer cancel()
	if len(backlog) != 3 || backlog[0].Message != "b" || backlog[2].Message != "d" || backlog[2].Seq != 4 {
		t.Fatalf("backlog = %+v", backlog)
	}
	h.Publish(LiveEvent{Message: "e"})
	if e := <-events; e.Message != "e" || e.Seq != 5 {
		t.Fatalf("live = %+v", e)
	}
}

func TestASlowSubscriberMissesEventsInsteadOfBlocking(t *testing.T) {
	h := NewLiveHub(10)
	_, _, cancel := h.Subscribe()
	defer cancel()
	for i := 0; i < 1000; i++ { // nobody reads: Publish must not block
		h.Publish(LiveEvent{Message: "x"})
	}
}

func TestLogRecordsReachTheHubWithTheirAttributes(t *testing.T) {
	var out bytes.Buffer
	log := slog.New(teeHandler{inner: slog.NewTextHandler(&out, nil)}).With("request_id", "r1")
	_, events, cancel := LiveLogs.Subscribe()
	defer cancel()
	log.Info("tool_started", "tool", "song")
	log.Debug("hidden") // below the inner handler's level: neither printed nor sent
	log.Info("reasoning_chunk", "content", "hmm")
	e := <-events
	if e.Message != "tool_started" || e.Level != "INFO" || e.Attrs["tool"] != "song" || e.Attrs["request_id"] != "r1" {
		t.Fatalf("event = %+v", e)
	}
	select {
	case e := <-events:
		t.Fatalf("unexpected %+v", e)
	default:
	}
	if !bytes.Contains(out.Bytes(), []byte("tool_started")) || !bytes.Contains(out.Bytes(), []byte("reasoning_chunk")) {
		t.Fatalf("the real handler lost records: %s", out.String())
	}
}
