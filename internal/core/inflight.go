// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Inflight is one request under WithRequestLock: waiting for its turn, or running.
type Inflight struct {
	Key       string    `json:"key"`
	Operation string    `json:"operation"`
	Source    string    `json:"source"`
	Since     time.Time `json:"since"`
	Running   bool      `json:"running"`
}

var (
	inflight    sync.Map // uint64 -> *Inflight
	inflightSeq atomic.Uint64
	inflightMu  sync.Mutex // guards Running flips against snapshot reads
)

func trackInflight(key, operation, source string) uint64 {
	id := inflightSeq.Add(1)
	inflight.Store(id, &Inflight{Key: key, Operation: operation, Source: source, Since: time.Now()})
	return id
}

func markRunning(id uint64) {
	if v, ok := inflight.Load(id); ok {
		inflightMu.Lock()
		v.(*Inflight).Running = true
		inflightMu.Unlock()
	}
}

func untrackInflight(id uint64) { inflight.Delete(id) }

// InflightRequests is every request now waiting or running, oldest first.
func InflightRequests() []Inflight {
	var out []Inflight
	inflightMu.Lock()
	inflight.Range(func(_, v any) bool {
		out = append(out, *v.(*Inflight))
		return true
	})
	inflightMu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Since.Before(out[j].Since) })
	return out
}
