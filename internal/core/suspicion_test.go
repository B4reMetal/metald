// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"math"
	"sync"
	"testing"
	"time"
)

func fresh() *Suspicion { return &Suspicion{byNick: map[string]mark{}} }

// One signal must never be enough. The whole point of a score is that a
// single refused tool call is a person testing a boundary, not a campaign.
func TestSingleSignalDoesNotReachQuarantine(t *testing.T) {
	s := fresh()
	for _, w := range []float64{SignalToolRefused, SignalScreenDenied,
		SignalInjectionFrames, SignalToolSyntax, SignalRunaway, SignalReplyDenied} {
		s2 := fresh()
		if got := s2.Add("", "greg", w); got >= SuspicionQuarantine {
			t.Errorf("weight %v alone reached the threshold (%v)", w, got)
		}
	}
	_ = s
}

// A campaign does reach it. This is the live sequence: repeated refused tool
// calls with syntax latches alongside.
func TestSustainedProbingReachesQuarantine(t *testing.T) {
	s := fresh()
	var score float64
	for i := 0; i < 2; i++ {
		score = s.Add("", "greg", SignalToolRefused)
	}
	if score < SuspicionQuarantine {
		score = s.Add("", "greg", SignalToolSyntax)
	}
	if score < SuspicionQuarantine {
		t.Errorf("two refusals and a latch should trip quarantine, got %v", score)
	}
}

// Scores decay, so yesterday's probing does not condemn today's question.
func TestScoreDecays(t *testing.T) {
	s := fresh()
	s.byNick["greg"] = mark{score: 4.0, at: time.Now().Add(-suspicionHalfLife)}

	got := s.Score("", "greg")
	if math.Abs(got-2.0) > 0.05 {
		t.Errorf("after one half-life 4.0 should be ~2.0, got %v", got)
	}
	if got >= SuspicionQuarantine {
		t.Error("a decayed score should have dropped below the threshold")
	}
}

// Decayed-out entries are forgotten, so the map cannot grow without bound in
// a process that runs for months.
func TestColdEntriesAreSwept(t *testing.T) {
	s := fresh()
	for _, n := range []string{"a", "b", "c"} {
		s.byNick[n] = mark{score: 3, at: time.Now().Add(-40 * suspicionHalfLife)}
	}
	s.Add("", "live", SignalToolSyntax)

	if len(s.byNick) != 1 {
		t.Errorf("cold entries survived: %v", s.byNick)
	}
	if s.Score("", "live") == 0 {
		t.Error("the live entry was swept")
	}
}

// Scores are per speaker. One person probing must not raise anyone else's.
func TestScoresDoNotBleedBetweenSpeakers(t *testing.T) {
	s := fresh()
	for i := 0; i < 5; i++ {
		s.Add("", "greg", SignalToolRefused)
	}
	if s.Score("", "alice") != 0 {
		t.Errorf("an unrelated speaker scored %v", s.Score("", "alice"))
	}
}

// Discount leaves someone below the threshold but not innocent.
func TestDiscountAfterActing(t *testing.T) {
	s := fresh()
	for i := 0; i < 3; i++ {
		s.Add("", "greg", SignalToolRefused)
	}
	before := s.Score("", "greg")
	s.Discount("", "greg", SuspicionQuarantine)
	after := s.Score("", "greg")

	if after >= SuspicionQuarantine {
		t.Errorf("discount left the score at %v, still over threshold", after)
	}
	if after >= before {
		t.Error("discount did not reduce the score")
	}
}

// An unattributed event must not accumulate under a blank key.
func TestEmptyNickIgnored(t *testing.T) {
	s := fresh()
	s.Add("", "", SignalToolRefused)
	if len(s.byNick) != 0 {
		t.Error("a blank nick was recorded")
	}
}

func TestConcurrentAddIsSafe(t *testing.T) {
	s := fresh()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); s.Add("", "greg", SignalToolSyntax) }()
	}
	wg.Wait()
	if s.Score("", "greg") <= 0 {
		t.Error("concurrent adds recorded nothing")
	}
}
