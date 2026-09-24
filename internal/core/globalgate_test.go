// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The property multi-network was built around: two DIFFERENT conversations, on different networks,
// must not run model work at the same time.
func TestDifferentKeysStillSerializeGlobally(t *testing.T) {
	var concurrent, maxConcurrent int32
	var wg sync.WaitGroup

	keys := []string{"live/#chat", "testbed/#test", "live/#other", "testbed/#dev"}
	for _, k := range keys {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			WithRequestLock(ctx, key, "test", func() {
				n := atomic.AddInt32(&concurrent, 1)
				for {
					m := atomic.LoadInt32(&maxConcurrent)
					if n <= m || atomic.CompareAndSwapInt32(&maxConcurrent, m, n) {
						break
					}
				}
				time.Sleep(25 * time.Millisecond)
				atomic.AddInt32(&concurrent, -1)
			}, nil)
		}(k)
	}
	wg.Wait()

	if maxConcurrent > 1 {
		t.Errorf("%d requests ran concurrently across networks; the model must serve one at a time", maxConcurrent)
	}
}

// All four still have to actually run. A gate that serializes by dropping
// work would pass the test above and be useless.
func TestGlobalGateRunsEveryRequest(t *testing.T) {
	var ran int32
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			WithRequestLock(ctx, string(rune('a'+i)), "test", func() {
				atomic.AddInt32(&ran, 1)
				time.Sleep(2 * time.Millisecond)
			}, nil)
		}(i)
	}
	wg.Wait()
	if ran != 6 {
		t.Errorf("expected all 6 requests to run, got %d", ran)
	}
}

// The gate must be released even when the body panics, or one bad request
// wedges every network permanently.
func TestGlobalGateReleasedOnPanic(t *testing.T) {
	func() {
		defer func() { _ = recover() }()
		ctx := context.Background()
		WithRequestLock(ctx, "panicky", "test", func() { panic("boom") }, nil)
	}()

	done := make(chan struct{})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		WithRequestLock(ctx, "after", "test", func() { close(done) }, nil)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("global gate was never released after a panic; the bot would be wedged")
	}
}

// A request whose context dies while queued must not leave the gate held.
func TestGlobalGateTimeoutDoesNotLeak(t *testing.T) {
	release := make(chan struct{})
	holding := make(chan struct{})
	go func() {
		WithRequestLock(context.Background(), "holder", "test", func() {
			close(holding)
			<-release
		}, nil)
	}()
	<-holding

	var timedOut bool
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	WithRequestLock(ctx, "waiter", "test", func() { t.Error("should not have run") },
		func() { timedOut = true })
	cancel()
	if !timedOut {
		t.Error("expected the queued request to time out")
	}

	close(release)

	done := make(chan struct{})
	go func() {
		c, cn := context.WithTimeout(context.Background(), 2*time.Second)
		defer cn()
		WithRequestLock(c, "next", "test", func() { close(done) }, nil)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("gate leaked after a timeout")
	}
}

func TestConcurrencyLimitAdmitsNAtOnce(t *testing.T) {
	SetConcurrency(3)
	defer SetConcurrency(1)
	l := NewRequestLock()
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if !l.LockWithContext(ctx) {
			t.Fatalf("slot %d should be free", i)
		}
	}
	short, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if l.LockWithContext(short) {
		t.Fatal("fourth request must wait")
	}
	l.Unlock()
	if !l.LockWithContext(ctx) {
		t.Fatal("a released slot must admit the next request")
	}
}

func TestRaisingConcurrencyWakesWaiters(t *testing.T) {
	SetConcurrency(1)
	defer SetConcurrency(1)
	l := GetRequestLock("raise-test")
	ctx := context.Background()
	l.LockWithContext(ctx)
	got := make(chan bool, 1)
	go func() { got <- l.LockWithContext(ctx) }()
	time.Sleep(30 * time.Millisecond)
	SetConcurrency(2)
	select {
	case ok := <-got:
		if !ok {
			t.Fatal("waiter should have been admitted")
		}
	case <-time.After(time.Second):
		t.Fatal("raising the limit did not wake the waiter")
	}
}

func TestCancelledWaiterDoesNotLeakSlot(t *testing.T) {
	SetConcurrency(1)
	l := NewRequestLock()
	ctx := context.Background()
	l.LockWithContext(ctx)
	short, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	l.LockWithContext(short)
	l.Unlock()
	if l.Held() != 0 {
		t.Fatalf("slot leaked: held=%d", l.Held())
	}
}
