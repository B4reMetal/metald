// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"sync"
	"time"
)

// PromptOverrides tracks channels running a user-supplied system prompt.
type PromptOverrides struct {
	mu    sync.RWMutex
	byKey map[string]PromptOverride
}

// PromptOverride is one channel's user-supplied prompt.
type PromptOverride struct {
	Prompt string // the user's text, without the safety floor
	Source string // who set it
	Set    time.Time
}

var (
	globalPrompts     *PromptOverrides
	globalPromptsOnce sync.Once
)

// Prompts returns the process-wide registry.
func Prompts() *PromptOverrides {
	globalPromptsOnce.Do(func() {
		globalPrompts = &PromptOverrides{byKey: map[string]PromptOverride{}}
	})
	return globalPrompts
}

// Set records a user prompt for a channel.
func (p *PromptOverrides) Set(key, prompt, source string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.byKey[key] = PromptOverride{Prompt: prompt, Source: source, Set: time.Now()}
}

// Get returns the override for a channel, if any.
func (p *PromptOverrides) Get(key string) (PromptOverride, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	o, ok := p.byKey[key]
	return o, ok
}

// Active reports whether a channel is running a user prompt, and so must be
// served without tools.
func (p *PromptOverrides) Active(key string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	_, ok := p.byKey[key]
	return ok
}

// Clear drops a channel's override, reporting whether there was one.
func (p *PromptOverrides) Clear(key string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.byKey[key]
	delete(p.byKey, key)
	return ok
}
