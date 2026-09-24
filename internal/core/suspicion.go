// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"math"
	"sort"
	"sync"
	"time"
)

// Suspicion tracks, per speaker, how much evidence has accumulated that they are probing the bot
// rather than talking to it.
type Suspicion struct {
	mu     sync.Mutex
	byNick map[string]mark
}

type mark struct {
	score float64
	at    time.Time
}

// suspicionHalfLife is how long it takes an untouched score to halve.
const suspicionHalfLife = 10 * time.Minute

// suspicionFloor is the score below which an entry is forgotten entirely, which is also what bounds
// the map: nicks decay out instead of accumulating forever in a long-running process.
const suspicionFloor = 0.05

// Signal weights. Ordered by how much each one actually distinguishes an
// attack from ordinary use, which is not the same as how alarming it looks.
const (
	SignalToolRefused = 1.5

	// The classifier refused the message outright.
	SignalScreenDenied = 1.0

	// Fake think/tool tags in a user message. There is no innocent reason to
	// send these; the payload is whatever they were wrapped around.
	SignalInjectionFrames = 1.0

	// An attempt to write an instruction into permanent memory.
	SignalMemoryRefused = 1.0

	// The model wrote tool-call wire format as prose. Often just confusion,
	// so it is weighted as corroboration rather than evidence.
	SignalToolSyntax = 0.5

	// A runaway turn. Same reasoning: a symptom that an attack produces, but
	// so does a model having a bad day.
	SignalRunaway = 0.5

	SignalReplyDenied = 2.0
)

// SuspicionQuarantine is the score at which a speaker's own history is dropped.
const SuspicionQuarantine = 3.0

var (
	globalSuspicion     *Suspicion
	globalSuspicionOnce sync.Once
)

// Suspicions returns the process-wide registry.
func Suspicions() *Suspicion {
	globalSuspicionOnce.Do(func() {
		globalSuspicion = &Suspicion{byNick: map[string]mark{}}
	})
	return globalSuspicion
}

// decayed returns what a mark is worth now.
func (m mark) decayed(now time.Time) float64 {
	if m.score == 0 {
		return 0
	}
	elapsed := now.Sub(m.at)
	if elapsed <= 0 {
		return m.score
	}
	return m.score * math.Pow(0.5, float64(elapsed)/float64(suspicionHalfLife))
}

// Add records a signal against a speaker and returns their new score.
func (s *Suspicion) Add(network, nick string, weight float64) float64 {
	if nick == "" || weight <= 0 {
		return 0
	}
	nick = ScopeKey(network, nick)
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	score := s.byNick[nick].decayed(now) + weight
	s.byNick[nick] = mark{score: score, at: now}
	s.sweep(now)
	return score
}

// Score returns a speaker's current, decayed score without recording
// anything.
func (s *Suspicion) Score(network, nick string) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byNick[ScopeKey(network, nick)].decayed(time.Now())
}

// Clear forgets a speaker entirely.
func (s *Suspicion) Clear(network, nick string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byNick, ScopeKey(network, nick))
}

// Discount reduces a speaker's score, used after acting on it so the same
// accumulated evidence does not fire again on the very next message.
func (s *Suspicion) Discount(network, nick string, by float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	nick = ScopeKey(network, nick)
	now := time.Now()
	score := s.byNick[nick].decayed(now) - by
	if score < suspicionFloor {
		delete(s.byNick, nick)
		return
	}
	s.byNick[nick] = mark{score: score, at: now}
}

// Snapshot returns one network's current scores, highest first.
func (s *Suspicion) Snapshot(network string) []ScoredNick {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	out := make([]ScoredNick, 0, len(s.byNick))
	for nick, m := range s.byNick {
		if !keyInNetwork(network, nick) {
			continue
		}
		if d := m.decayed(now); d >= suspicionFloor {
			out = append(out, ScoredNick{Nick: UnscopeKey(network, nick), Score: d})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

// ScoredNick is one row of a Snapshot.
type ScoredNick struct {
	Nick  string
	Score float64
}

// sweep drops entries that have decayed to nothing.
func (s *Suspicion) sweep(now time.Time) {
	for nick, m := range s.byNick {
		if m.decayed(now) < suspicionFloor {
			delete(s.byNick, nick)
		}
	}
}
