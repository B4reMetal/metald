// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package llm

import (
	"sort"
	"strings"
	"sync"

	"github.com/alexschlessinger/pollytool/tools"

	"B4reMetal/metald/internal/irc"
)

// Chat, background work and a delegate see different tools: the checklist, note and delegate tools
// exist only inside background work, tools that start background work only outside it, and
// channel-management tools never inside it. A delegate gets the work's tools minus its own
// management ones, so it cannot delegate further. Each view reuses the loaded tool objects, sandbox
// included, and is rebuilt only when the set of loaded tools changes.

// toolScope is which view a conversation gets.
type toolScope int

const (
	scopeChat toolScope = iota
	scopeWork
	scopeDelegate
)

// scopeOf reads a conversation's scope from its session name.
func scopeOf(sessionName string) toolScope {
	switch {
	case strings.HasPrefix(sessionName, delegatePrefix):
		return scopeDelegate
	case irc.TaskIDFromSession(sessionName) != 0:
		return scopeWork
	}
	return scopeChat
}

type toolViews struct {
	mu                   sync.Mutex
	base                 *tools.ToolRegistry
	signature            string
	chat, work, delegate *tools.ToolRegistry
}

var views toolViews

// toolView returns the tools a conversation in scope may use.
func toolView(base *tools.ToolRegistry, scope toolScope) *tools.ToolRegistry {
	if base == nil {
		return nil
	}
	all := base.All()
	names := make([]string, len(all))
	for i, t := range all {
		names[i] = t.GetName()
	}
	sort.Strings(names)
	sig := strings.Join(names, ",")

	views.mu.Lock()
	defer views.mu.Unlock()
	if views.base != base || views.signature != sig {
		var chat, work, delegate []tools.Tool
		for _, t := range all {
			name := t.GetName()
			if !irc.TaskOnlyTools[name] {
				chat = append(chat, t)
			}
			if !irc.ChatOnlyTools[name] && !irc.NotInTasks[name] {
				work = append(work, t)
				if !irc.TaskOnlyTools[name] {
					delegate = append(delegate, t)
				}
			}
		}
		views.base, views.signature = base, sig
		views.chat, views.work, views.delegate = tools.NewToolRegistry(chat), tools.NewToolRegistry(work), tools.NewToolRegistry(delegate)
	}
	switch scope {
	case scopeWork:
		return views.work
	case scopeDelegate:
		return views.delegate
	}
	return views.chat
}
