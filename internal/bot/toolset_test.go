// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package bot

import (
	"slices"
	"testing"
)

func TestTaskToolsetComesWithTaskStart(t *testing.T) {
	got := withTaskToolset([]string{"plugins/paste.py", "task__start"})
	for _, want := range []string{"task__schedule", "goal__propose", "todo__set", "todo__update", "task__note", "task__delegate"} {
		if !slices.Contains(got, want) {
			t.Errorf("%s not loaded with task__start: %v", want, got)
		}
	}
	if got := withTaskToolset([]string{"plugins/paste.py"}); len(got) != 1 {
		t.Errorf("toolset added without task__start: %v", got)
	}
	if got := withTaskToolset([]string{"task__start", "todo__set"}); len(got) != 7 {
		t.Errorf("duplicates: %v", got)
	}
}
