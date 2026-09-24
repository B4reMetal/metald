// Copyright (C) 2023-2026 Alex Schlessinger and soulshack contributors
// Modified 2026 by BareMetal
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"context"
	"log/slog"
	"sync"
)

// RequestLock is a counting semaphore whose limit follows SetConcurrency; at 1 it is a mutex.
type RequestLock struct {
	mu      sync.Mutex
	held    int
	waiters []chan struct{}
}

// NewRequestLock creates a new request lock
func NewRequestLock() *RequestLock {
	return &RequestLock{}
}

var (
	concurrencyMu sync.RWMutex
	concurrency   = 1
)

// Concurrency is how many requests may hold a lock at once.
func Concurrency() int {
	concurrencyMu.RLock()
	defer concurrencyMu.RUnlock()
	return concurrency
}

// SetConcurrency changes the limit for the global gate and every per-key
// lock. Raising it wakes waiters immediately; lowering it lets current
// holders finish and admits no one new until the count drops below it.
func SetConcurrency(n int) {
	if n < 1 {
		n = 1
	}
	concurrencyMu.Lock()
	concurrency = n
	concurrencyMu.Unlock()
	globalGate.admit()
	requestLocks.Range(func(_, v any) bool {
		v.(*RequestLock).admit()
		return true
	})
}

// LockWithContext attempts to acquire the lock, respecting context cancellation
func (c *RequestLock) LockWithContext(ctx context.Context) bool {
	c.mu.Lock()
	if c.held < Concurrency() && len(c.waiters) == 0 {
		c.held++
		c.mu.Unlock()
		return true
	}
	ch := make(chan struct{})
	c.waiters = append(c.waiters, ch)
	c.mu.Unlock()

	select {
	case <-ch:
		return true
	case <-ctx.Done():
		c.mu.Lock()
		for i, w := range c.waiters {
			if w == ch {
				c.waiters = append(c.waiters[:i], c.waiters[i+1:]...)
				c.mu.Unlock()
				return false
			}
		}
		c.mu.Unlock()
		// Granted in the same instant it was cancelled: give the slot back.
		c.Unlock()
		return false
	}
}

// Unlock releases one slot.
func (c *RequestLock) Unlock() {
	c.mu.Lock()
	if c.held > 0 {
		c.held--
	}
	c.mu.Unlock()
	c.admit()
}

func (c *RequestLock) admit() {
	limit := Concurrency()
	c.mu.Lock()
	defer c.mu.Unlock()
	for c.held < limit && len(c.waiters) > 0 {
		c.held++
		close(c.waiters[0])
		c.waiters = c.waiters[1:]
	}
}

// IsHeld reports whether any request holds the lock.
func (c *RequestLock) IsHeld() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.held > 0
}

// Held is how many requests currently hold the lock.
func (c *RequestLock) Held() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.held
}

var requestLocks sync.Map

// globalGate bounds model work across EVERY conversation and network to
// Concurrency() requests at once.
var globalGate = NewRequestLock()

// GlobalGateHeld reports whether some request currently holds the model.
func GlobalGateHeld() bool { return globalGate.IsHeld() }

// ResetRequestLock makes key immediately acquirable again, even if a wedged request still holds it.
func ResetRequestLock(key string) bool {
	held := false
	if existing, ok := requestLocks.Load(key); ok {
		held = existing.(*RequestLock).IsHeld()
	}
	requestLocks.Store(key, NewRequestLock())
	return held
}

// GetRequestLock returns the lock for a given key, creating it if needed
func GetRequestLock(key string) *RequestLock {
	if lock, ok := requestLocks.Load(key); ok {
		return lock.(*RequestLock)
	}

	// Create new lock for this key
	newLock := NewRequestLock()
	actual, _ := requestLocks.LoadOrStore(key, newLock)
	return actual.(*RequestLock)
}

// WithRequestLock acquires a lock for the given key and executes the onSuccess function.
// If the lock cannot be acquired within the context's deadline, onTimeout is called (if provided).
func WithRequestLock(ctx context.Context, key string, operation string, onSuccess func(), onTimeout func()) {
	lock := GetRequestLock(key)

	// Try to get logger from context, fallback to global logger
	var logger *slog.Logger
	if logCtx, ok := ctx.(interface{ GetLogger() *slog.Logger }); ok {
		logger = logCtx.GetLogger()
	} else {
		logger = GetLogger()
	}

	logger.Debug("lock_acquiring", "lock_key", key, "operation", operation)
	if !lock.LockWithContext(ctx) {
		logger.Warn("lock_timeout", "lock_key", key, "operation", operation)
		if onTimeout != nil {
			onTimeout()
		}
		return
	}
	logger.Debug("lock_acquired", "lock_key", key, "operation", operation)
	defer func() {
		logger.Debug("lock_released", "lock_key", key, "operation", operation)
		lock.Unlock()
	}()

	// Then the process-wide gate: one request at the model at a time,
	// whichever network it came from.
	if !globalGate.LockWithContext(ctx) {
		logger.Warn("global_gate_timeout", "lock_key", key, "operation", operation)
		if onTimeout != nil {
			onTimeout()
		}
		return
	}
	defer globalGate.Unlock()

	onSuccess()
}

var commitLocks sync.Map

// CommitLock serializes writes to one session's history, so a finished
// exchange lands as one contiguous block even when requests overlap.
func CommitLock(session any) *sync.Mutex {
	m, _ := commitLocks.LoadOrStore(session, &sync.Mutex{})
	return m.(*sync.Mutex)
}
