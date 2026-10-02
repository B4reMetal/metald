// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package llm

import (
	"net/url"
	"regexp"
	"strings"
	"sync"
	"unicode"

	"github.com/alexschlessinger/pollytool/messages"

	"B4reMetal/metald/internal/irc"
)

// A model can write a link to its own hosting before the tool that would create it has run, and
// the link then points at nothing. Hosts the bot's tools publish to are learned from their "url: "
// result lines; a reply may only link to such a host if the link appeared in its conversation or in
// a tool result during the request.

var (
	linkPattern = regexp.MustCompile(`https?://[^\s<>"'` + "`" + `]+`)
	spaceBefore = regexp.MustCompile(`\s+([.,;:!?])`)
	ownHosts    sync.Map // host -> struct{}
	vouched     sync.Map // request id -> *linkSet
)

type linkSet struct {
	mu   sync.Mutex
	urls map[string]bool
}

// cleanLink trims punctuation a sentence leaves stuck to the end of a link.
func cleanLink(u string) string {
	return strings.TrimRight(u, ".,;:!?)]}'\"")
}

func linkHost(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return ""
	}
	return strings.ToLower(p.Hostname())
}

// learnOwnHosts records the host of every "url: " line in a tool result.
func learnOwnHosts(result string) {
	for _, line := range strings.Split(result, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "url: "); ok {
			if h := linkHost(cleanLink(strings.TrimSpace(rest))); h != "" {
				ownHosts.Store(h, struct{}{})
			}
		}
	}
}

// vouchLinks allows a request to repeat every link in text.
func vouchLinks(requestID, text string) {
	found := linkPattern.FindAllString(text, -1)
	if len(found) == 0 {
		return
	}
	v, _ := vouched.LoadOrStore(requestID, &linkSet{urls: map[string]bool{}})
	set := v.(*linkSet)
	set.mu.Lock()
	defer set.mu.Unlock()
	for _, u := range found {
		set.urls[cleanLink(u)] = true
	}
}

// vouchConversation allows the links a request's conversation already holds, and learns hosts from
// the tool results in it.
func vouchConversation(requestID string, msgs []messages.ChatMessage) {
	for _, m := range msgs {
		if m.Role == messages.MessageRoleSystem {
			continue
		}
		c := m.GetContent()
		if m.Role == messages.MessageRoleTool {
			learnOwnHosts(c)
		}
		vouchLinks(requestID, c)
	}
}

// isVouched reports whether u, or a link it extends (a gist's raw path), was vouched for.
func isVouched(requestID, u string) bool {
	v, ok := vouched.Load(requestID)
	if !ok {
		return false
	}
	set := v.(*linkSet)
	set.mu.Lock()
	defer set.mu.Unlock()
	if set.urls[u] {
		return true
	}
	for known := range set.urls {
		if strings.HasPrefix(u, strings.TrimRight(known, "/")+"/") {
			return true
		}
	}
	return false
}

// guardLinks removes links to the bot's own hosts that nothing vouched for, and reports whether
// anything worth posting is left of the line.
func guardLinks(ctx irc.ChatContextInterface, line string) (string, bool) {
	changed := false
	out := linkPattern.ReplaceAllStringFunc(line, func(raw string) string {
		u := cleanLink(raw)
		if _, own := ownHosts.Load(linkHost(u)); !own || isVouched(ctx.GetRequestID(), u) {
			return raw
		}
		ctx.GetLogger().Warn("unvouched_link_dropped", "url", u)
		changed = true
		return strings.TrimPrefix(raw, u)
	})
	if !changed {
		return line, true
	}
	out = spaceBefore.ReplaceAllString(strings.Join(strings.Fields(out), " "), "$1")
	for _, r := range out {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return out, true
		}
	}
	return "", false
}

// forgetLinks drops a finished request's vouched links.
func forgetLinks(requestID string) { vouched.Delete(requestID) }
