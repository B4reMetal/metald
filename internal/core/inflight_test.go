// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"context"
	"testing"
)

type sourced struct {
	context.Context
	source string
}

func (s sourced) GetSource() string { return s.source }

func TestInflightRequestsFollowTheLock(t *testing.T) {
	var during []Inflight
	WithRequestLock(sourced{context.Background(), "alice"}, "test/#chat", "addressed", func() {
		during = InflightRequests()
	}, nil)
	var mine []Inflight
	for _, r := range during {
		if r.Key == "test/#chat" {
			mine = append(mine, r)
		}
	}
	if len(mine) != 1 || !mine[0].Running || mine[0].Source != "alice" || mine[0].Operation != "addressed" {
		t.Fatalf("during the request: %+v", mine)
	}
	for _, r := range InflightRequests() {
		if r.Key == "test/#chat" {
			t.Fatalf("still listed after it finished: %+v", r)
		}
	}
}
