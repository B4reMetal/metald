// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package irc

import (
	"regexp"
	"strings"
)

// Reasoning markers a model uses when it writes its thinking into the content stream instead of the
// reasoning channel.
var (
	thinkOpen  = regexp.MustCompile(`(?i)(<think(ing)?>|\[think(ing)?\])`)
	thinkClose = regexp.MustCompile(`(?i)(</think(ing)?>|\[/think(ing)?\])`)

	// The full set, for the hold-back check. Longest is 11 bytes.
	thinkMarkers = []string{
		"<think>", "<thinking>", "</think>", "</thinking>",
		"[think]", "[thinking]", "[/think]", "[/thinking]",
	}
)

// markerHold is how many trailing bytes Feed may retain between calls: enough
// to complete the longest marker if a chunk boundary fell inside it.
const markerHold = 12

// ReasoningFilter removes think blocks from streamed content.
type ReasoningFilter struct {
	pending string
	inside  bool

	// Blocks counts opened think blocks, Stray counts closers seen with no
	// opener. Both exist so the caller can log that something was stripped.
	Blocks int
	Stray  int
}

// Feed accepts the next chunk and returns the visible text it releases.
func (f *ReasoningFilter) Feed(s string) string {
	f.pending += s
	var out strings.Builder
	for {
		if f.inside {
			m := thinkClose.FindStringIndex(f.pending)
			if m == nil {
				// Discard the block as it arrives, keeping only a tail that
				// could be the front half of a split closer.
				if len(f.pending) > markerHold {
					f.pending = f.pending[len(f.pending)-markerHold:]
				}
				return out.String()
			}
			f.pending = strings.TrimLeft(f.pending[m[1]:], " \t\r\n")
			f.inside = false
			continue
		}

		o := thinkOpen.FindStringIndex(f.pending)
		c := thinkClose.FindStringIndex(f.pending)

		// A closer with no opener means the reasoning had no marker at all and has already gone out as
		// content.
		if c != nil && (o == nil || c[0] < o[0]) {
			out.WriteString(f.pending[:c[0]])
			f.pending = strings.TrimLeft(f.pending[c[1]:], " \t\r\n")
			f.Stray++
			continue
		}
		if o != nil {
			out.WriteString(f.pending[:o[0]])
			f.pending = f.pending[o[1]:]
			f.inside = true
			f.Blocks++
			continue
		}

		cut := holdBack(f.pending)
		out.WriteString(f.pending[:cut])
		f.pending = f.pending[cut:]
		return out.String()
	}
}

// Flush returns the held-back tail at end of turn. Inside a block that never
// closed, everything is reasoning and nothing comes out.
func (f *ReasoningFilter) Flush() string {
	p := f.pending
	f.pending = ""
	if f.inside {
		return ""
	}
	return p
}

// holdBack returns how much of s can be released now.
func holdBack(s string) int {
	start := max(len(s)-markerHold, 0)
	tail := s[start:]
	idx := strings.LastIndexAny(tail, "<[")
	if idx == -1 {
		return len(s)
	}
	cand := strings.ToLower(tail[idx:])
	for _, m := range thinkMarkers {
		if strings.HasPrefix(m, cand) {
			return start + idx
		}
	}
	return len(s)
}

// bareThinkMarker matches a line that is nothing but a reasoning marker, the
// shape a leaked block leaves behind when the stream filter is bypassed.
var bareThinkMarker = regexp.MustCompile(`(?i)^\s*(</?think(ing)?>|\[/?think(ing)?\])\s*$`)
