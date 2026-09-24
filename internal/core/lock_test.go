// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"context"
	"testing"
	"time"
)

func TestResetRequestLockFreesHeldLock(t *testing.T) {
	key := "#reset-held"
	lock := GetRequestLock(key)
	if !lock.LockWithContext(context.Background()) {
		t.Fatal("should have acquired a fresh lock")
	}
	if !lock.IsHeld() {
		t.Fatal("lock should report held after acquire")
	}

	if !ResetRequestLock(key) {
		t.Fatal("ResetRequestLock should report the lock was held")
	}

	// The replacement lock must be immediately acquirable even though the
	// original holder never released - that's the whole point.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if !GetRequestLock(key).LockWithContext(ctx) {
		t.Fatal("lock should be acquirable immediately after reset")
	}
}

func TestResetRequestLockReportsUnheld(t *testing.T) {
	key := "#reset-unheld"
	GetRequestLock(key) // create, but never acquire
	if ResetRequestLock(key) {
		t.Fatal("ResetRequestLock should report false when nothing held it")
	}
}

// The stuck request's own deferred Unlock must not release the NEW lock - otherwise a third request
// could slip in alongside a second, and each spurious release would admit one more, cascading.
func TestStuckHolderUnlockDoesNotFreeNewLock(t *testing.T) {
	key := "#reset-cascade"
	stuck := GetRequestLock(key)
	if !stuck.LockWithContext(context.Background()) {
		t.Fatal("failed to acquire initial lock")
	}

	ResetRequestLock(key)

	fresh := GetRequestLock(key)
	if !fresh.LockWithContext(context.Background()) {
		t.Fatal("failed to acquire replacement lock")
	}

	// The wedged request finally finishes and unlocks. It holds a reference
	// to the OLD lock, so this must not free the new one.
	stuck.Unlock()

	if !fresh.IsHeld() {
		t.Fatal("stuck holder's Unlock released the replacement lock - cascade bug")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if GetRequestLock(key).LockWithContext(ctx) {
		t.Fatal("a second request got in concurrently after the stuck holder unlocked")
	}
}
