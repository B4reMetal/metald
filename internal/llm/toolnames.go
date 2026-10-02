// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package llm

import (
	"strings"

	"github.com/alexschlessinger/pollytool/messages"
	"github.com/alexschlessinger/pollytool/tools"
)

// The model mangles tool names: wrong case ("imagegen__Picture"), the plugin alone ("websearch",
// "slap"), or the plugin with the separator and nothing after it ("imagegen__").

// resolveToolName returns the one registered tool a mangled name can only mean, or want unchanged
// when it is already exact, unknown, or ambiguous.
func resolveToolName(names []string, want string) string {
	for _, n := range names {
		if n == want {
			return want
		}
	}
	lw := strings.ToLower(strings.TrimSpace(want))
	for _, n := range names {
		if strings.ToLower(n) == lw {
			return n
		}
	}

	// A bare plugin name, or one ending in the separator, means that plugin's only tool. A bare tool
	// name means the only plugin tool with that name.
	plugin := strings.TrimRight(lw, "_")
	var matches []string
	for _, n := range names {
		ln := strings.ToLower(n)
		p, t, ok := strings.Cut(ln, "__")
		if !ok {
			continue
		}
		if p == plugin || (!strings.Contains(lw, "__") && t == lw) {
			matches = append(matches, n)
		}
	}
	if len(matches) == 1 {
		return matches[0]
	}
	return want
}

func registryNames(r *tools.ToolRegistry) []string {
	if r == nil {
		return nil
	}
	var names []string
	for _, t := range r.All() {
		names = append(names, t.GetName())
	}
	return names
}

// correctToolNames rewrites mangled names in place, before the agent looks the tools up.
func (h *callbackHandler) correctToolNames(calls []messages.ChatMessageToolCall) {
	sys := h.chatCtx.GetSystem()
	if sys == nil {
		return
	}
	names := registryNames(toolView(sys.GetToolRegistry(), scopeOf(h.chatCtx.GetSession().GetName())))
	for i := range calls {
		if fixed := resolveToolName(names, calls[i].Name); fixed != calls[i].Name {
			h.chatCtx.GetLogger().Info("tool_name_corrected", "from", calls[i].Name, "to", fixed)
			calls[i].Name = fixed
		}
	}
}

// toolRetryNote tells the model, after it wrote a tool call as text, what the exact names are.
func toolRetryNote(note string, r *tools.ToolRegistry) string {
	return strings.ReplaceAll(strings.TrimSpace(note), "{tools}", strings.Join(registryNames(r), ", "))
}
