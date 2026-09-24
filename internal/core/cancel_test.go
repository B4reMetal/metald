// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"context"
	"testing"
	"time"
)

func tracked(t *testing.T, key, source string) (context.Context, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := Requests().Track(key, "req-"+source, source, cancel)
	t.Cleanup(done)
	return ctx, done
}

func TestCancelKeyStopsRequest(t *testing.T) {
	ctx, _ := tracked(t, "#chat", "mallory")

	if n := Requests().CancelKey("#chat", ""); n != 1 {
		t.Fatalf("cancelled %d, want 1", n)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("request context was not cancelled")
	}
}

// Cancelling one conversation must not disturb another - "+reset" in one
// channel should not kill work everywhere.
func TestCancelKeyIsScoped(t *testing.T) {
	a, _ := tracked(t, "#a", "u1")
	b, _ := tracked(t, "#b", "u2")

	Requests().CancelKey("#a", "")

	select {
	case <-a.Done():
	case <-time.After(time.Second):
		t.Fatal("#a should have been cancelled")
	}
	select {
	case <-b.Done():
		t.Fatal("#b was cancelled by a reset in #a")
	default:
	}
}

// Ignoring someone cancels THEIR work, not everyone else's in the channel.
func TestCancelSourceOnlyAffectsThatNick(t *testing.T) {
	pest, _ := tracked(t, "#chat", "Dave")
	other, _ := tracked(t, "#chat", "someone")

	if n := Requests().CancelSource("Dave", ""); n != 1 {
		t.Fatalf("cancelled %d, want 1", n)
	}
	select {
	case <-pest.Done():
	case <-time.After(time.Second):
		t.Fatal("the ignored nick's request should have been cancelled")
	}
	select {
	case <-other.Done():
		t.Fatal("an unrelated user's request was cancelled")
	default:
	}
}

// IRC nicks are case-insensitive, so an ignore must not be defeated by case.
func TestCancelSourceIsCaseInsensitive(t *testing.T) {
	ctx, _ := tracked(t, "#chat", "DAVE")
	if n := Requests().CancelSource("dave", ""); n != 1 {
		t.Fatalf("cancelled %d, want 1", n)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("case difference defeated the cancel")
	}
}

// Deregistration must actually remove the entry, or the map grows forever and
// a later cancel touches dead requests.
func TestDeregisterRemovesEntry(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := Requests().Track("#gone", "req-u", "u", cancel)

	if got := Requests().Count("#gone"); got != 1 {
		t.Fatalf("count = %d, want 1", got)
	}
	done()
	if got := Requests().Count("#gone"); got != 0 {
		t.Fatalf("count after deregister = %d, want 0", got)
	}
	if got := Requests().CancelKey("#gone", ""); got != 0 {
		t.Fatalf("cancelling a finished request returned %d", got)
	}
}

func TestCancelUnknownIsHarmless(t *testing.T) {
	if n := Requests().CancelKey("#nothing", ""); n != 0 {
		t.Errorf("got %d", n)
	}
	if n := Requests().CancelSource("nobody", ""); n != 0 {
		t.Errorf("got %d", n)
	}
}

// Several requests can be queued for one conversation; a reset stops them all.
func TestCancelKeyStopsEveryRequestForTheConversation(t *testing.T) {
	a, _ := tracked(t, "#many", "u1")
	b, _ := tracked(t, "#many", "u2")

	if n := Requests().CancelKey("#many", ""); n != 2 {
		t.Fatalf("cancelled %d, want 2", n)
	}
	for i, c := range []context.Context{a, b} {
		select {
		case <-c.Done():
		case <-time.After(time.Second):
			t.Fatalf("request %d not cancelled", i)
		}
	}
}

// "+reset" arrives as an event like any other and is therefore tracked like any other.
func TestCancelKeyExcludesTheCaller(t *testing.T) {
	selfCtx, selfCancel := context.WithCancel(context.Background())
	doneSelf := Requests().Track("#solo", "the-reset", "BareMetal", selfCancel)
	defer doneSelf()

	if n := Requests().CancelKey("#solo", "the-reset"); n != 0 {
		t.Fatalf("with only the caller running, CancelKey reported %d, want 0", n)
	}
	select {
	case <-selfCtx.Done():
		t.Fatal("the reset cancelled its own request")
	default:
	}
}

// ...but it must still cancel everything else in that conversation.
func TestCancelKeyStillCancelsOthers(t *testing.T) {
	victimCtx, victimCancel := context.WithCancel(context.Background())
	doneVictim := Requests().Track("#both", "the-victim", "someone", victimCancel)
	defer doneVictim()

	selfCtx, selfCancel := context.WithCancel(context.Background())
	doneSelf := Requests().Track("#both", "the-reset", "BareMetal", selfCancel)
	defer doneSelf()

	if n := Requests().CancelKey("#both", "the-reset"); n != 1 {
		t.Fatalf("cancelled %d, want exactly the one victim", n)
	}
	select {
	case <-victimCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("the real in-flight request was not cancelled")
	}
	select {
	case <-selfCtx.Done():
		t.Fatal("the caller cancelled itself")
	default:
	}
}

// Flood protection runs INSIDE a request from the nick it is timing out, so without excluding
// itself it cancels its own context and the "you are being ignored" announcement never gets sent.
func TestCancelSourceExcludesTheCaller(t *testing.T) {
	selfCtx, selfCancel := context.WithCancel(context.Background())
	doneSelf := Requests().Track("#chat", "the-flood-check", "mallory", selfCancel)
	defer doneSelf()

	if n := Requests().CancelSource("mallory", "the-flood-check"); n != 0 {
		t.Fatalf("cancelled %d, want 0 - only the caller was running", n)
	}
	select {
	case <-selfCtx.Done():
		t.Fatal("the flood check cancelled its own request")
	default:
	}
}

// ...but their OTHER in-flight requests must still be cancelled.
func TestCancelSourceStillCancelsTheirOtherRequests(t *testing.T) {
	otherCtx, otherCancel := context.WithCancel(context.Background())
	doneOther := Requests().Track("#chat", "earlier-request", "mallory", otherCancel)
	defer doneOther()

	selfCtx, selfCancel := context.WithCancel(context.Background())
	doneSelf := Requests().Track("#chat", "the-flood-check", "mallory", selfCancel)
	defer doneSelf()

	if n := Requests().CancelSource("mallory", "the-flood-check"); n != 1 {
		t.Fatalf("cancelled %d, want exactly the earlier request", n)
	}
	select {
	case <-otherCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("their earlier request was not cancelled")
	}
	select {
	case <-selfCtx.Done():
		t.Fatal("the caller cancelled itself")
	default:
	}
}
