// Copyright (C) 2023-2026 Alex Schlessinger and soulshack contributors
// Modified 2026 by BareMetal
// SPDX-License-Identifier: GPL-3.0-only

package behaviors

import (
	"testing"

	"github.com/lrstanley/girc"

	mocktest "B4reMetal/metald/internal/testing"
)

func TestOpBehaviorCheck(t *testing.T) {
	behavior := &OpBehavior{}

	tests := []struct {
		name   string
		params []string
		want   bool
	}{
		{
			name:   "matches +o for bot",
			params: []string{"#test", "+o", "metald"},
			want:   true,
		},
		{
			name:   "matches -o for bot",
			params: []string{"#test", "-o", "metald"},
			want:   true,
		},
		{
			name:   "ignores voice change for bot",
			params: []string{"#test", "+v", "metald"},
			want:   false,
		},
		{
			name:   "ignores op change for another user",
			params: []string{"#test", "+o", "someoneelse"},
			want:   false,
		},
		{
			name:   "handles mixed prefix modes before bot op",
			params: []string{"#test", "+qo", "chanowner", "metald"},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := mocktest.NewMockContext().WithConfig(mocktest.DefaultTestConfig())
			ctx.GetConfig().Bot.OpWatcher = true

			event := &girc.Event{
				Command: girc.MODE,
				Params:  tt.params,
			}

			if got := behavior.Check(ctx, event); got != tt.want {
				t.Fatalf("OpBehavior.Check(%v) = %v, want %v", tt.params, got, tt.want)
			}
		})
	}
}

func TestOpActionForNick(t *testing.T) {
	tests := []struct {
		name       string
		params     []string
		wantAction string
		wantOK     bool
	}{
		{
			name:       "returns opped action",
			params:     []string{"#test", "+o", "metald"},
			wantAction: "opped",
			wantOK:     true,
		},
		{
			name:       "returns deopped action",
			params:     []string{"#test", "-o", "metald"},
			wantAction: "deopped",
			wantOK:     true,
		},
		{
			name:       "ignores unrelated target mode",
			params:     []string{"#test", "+v", "metald"},
			wantAction: "",
			wantOK:     false,
		},
		{
			name:       "keeps arguments aligned across mixed modes",
			params:     []string{"#test", "+ov", "metald", "otheruser"},
			wantAction: "opped",
			wantOK:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &girc.Event{
				Command: girc.MODE,
				Params:  tt.params,
			}

			gotAction, gotOK := opActionForNick(event, "metald")
			if gotAction != tt.wantAction || gotOK != tt.wantOK {
				t.Fatalf("opActionForNick(%v) = (%q, %v), want (%q, %v)", tt.params, gotAction, gotOK, tt.wantAction, tt.wantOK)
			}
		})
	}
}

// The nick must come from the connection, not from a value captured when the behavior was
// registered.
func TestOpBehaviorUsesConnectionNick(t *testing.T) {
	behavior := &OpBehavior{}

	ctx := mocktest.NewMockContext()
	ctx.BotNick = "metalai" // this network's nick, not the first network's
	ctx.GetConfig().Bot.OpWatcher = true

	event := &girc.Event{
		Command: girc.MODE,
		Params:  []string{"#test", "+o", "metalai"},
		Source:  &girc.Source{Name: "someop"},
	}
	if !behavior.Check(ctx, event) {
		t.Error("should react to an op change on this connection's nick")
	}

	// And must NOT react to a different nick, even one another network uses.
	other := &girc.Event{
		Command: girc.MODE,
		Params:  []string{"#test", "+o", "BareMetal"},
		Source:  &girc.Source{Name: "someop"},
	}
	if behavior.Check(ctx, other) {
		t.Error("reacted to an op change on another network's nick")
	}
}
