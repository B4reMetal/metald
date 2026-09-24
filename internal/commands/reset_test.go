// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package commands

import (
	"strings"
	"testing"

	"B4reMetal/metald/internal/config"
	mocktest "B4reMetal/metald/internal/testing"
)

// +reset exists to unstick a wedged request lock, so it must be dispatched outside that lock.
func TestResetBypassesLock(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&ResetCommand{})

	if !registry.BypassesLock("+reset") {
		t.Fatal("+reset must bypass the request lock")
	}
}

// Named +commands run outside the lock so they stay usable during a long
// render; only chat to the model (the default command) queues.
func TestNamedCommandsBypassLock(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&GetCommand{})
	registry.Register(&IgnoreCommand{})
	registry.Register(&mockCommand{name: "+plain"})

	for _, name := range []string{"+get", "+ignore", "+plain"} {
		if !registry.BypassesLock(name) {
			t.Errorf("%s should bypass the request lock", name)
		}
	}
}

func TestDefaultCommandNeverBypassesLock(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&CompletionCommand{})
	for _, name := range []string{"", "metalai", "hello"} {
		if registry.BypassesLock(name) {
			t.Errorf("%q routed to the model must take the lock", name)
		}
	}
}

type lockedCommand struct{ mockCommand }

func (lockedCommand) RequiresLock() bool { return true }

func TestRequiresLockOptsBackIn(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&lockedCommand{mockCommand{name: "+heavy"}})
	if registry.BypassesLock("+heavy") {
		t.Fatal("a command that requires the lock must not bypass it")
	}
}

func TestBypassesLockUnknownCommand(t *testing.T) {
	registry := NewRegistry()
	if registry.BypassesLock("+nope") {
		t.Fatal("unknown command should not report lock bypass")
	}
}

// Deliberately usable by non-admins so anyone can unstick the bot.
func TestResetIsNotAdminOnly(t *testing.T) {
	if (&ResetCommand{}).AdminOnly() {
		t.Fatal("+reset should be available to everyone")
	}
}

// --- +reset restores the default model -----------------------------------

// withDefaultModel sets the captured config.yml default for one test, and
// points persistence at a temp file so tests don't write into the package dir.
func withDefaultModel(t *testing.T, def string) {
	t.Helper()

	defaultModelMu.Lock()
	prev := defaultModel
	defaultModel = def
	defaultModelMu.Unlock()

	prevPath := OverridesPath
	OverridesPath = t.TempDir() + "/config-overrides.json"

	t.Cleanup(func() {
		defaultModelMu.Lock()
		defaultModel = prev
		defaultModelMu.Unlock()
		OverridesPath = prevPath
	})
}

func TestResetRestoresDefaultModel(t *testing.T) {
	withDefaultModel(t, "openai/chat")

	ctx := mocktest.NewMockContext().WithSystem(mocktest.NewMockSystem())
	ctx.GetConfig().Model.Model = "openai/cydonia-24b"

	(&ResetCommand{}).Execute(ctx)

	if got := ctx.GetConfig().Model.Model; got != "openai/chat" {
		t.Errorf("expected the default model to be restored, got: %s", got)
	}
	if !strings.Contains(ctx.LastReply(), "Model restored to chat") {
		t.Errorf("reset should report the restore, got: %s", ctx.LastReply())
	}
}

// Dropping the persisted override matters: ApplyOverrides would otherwise bring the
// switched-to model back on the next restart.
func TestResetClearsPersistedModelOverride(t *testing.T) {
	withDefaultModel(t, "openai/chat")
	PersistSet("model", "openai/cydonia-24b")
	PersistSet("maxtokens", "2048") // an unrelated override must survive

	ctx := mocktest.NewMockContext().WithSystem(mocktest.NewMockSystem())
	ctx.GetConfig().Model.Model = "openai/cydonia-24b"

	(&ResetCommand{}).Execute(ctx)

	fresh := &config.Configuration{Model: &config.ModelConfig{Model: "openai/chat"}, API: &config.APIConfig{}}
	ApplyOverrides(fresh)
	if fresh.Model.Model != "openai/chat" {
		t.Errorf("model override survived reset; a restart would revert to: %s", fresh.Model.Model)
	}
	if fresh.Model.MaxTokens != 2048 {
		t.Errorf("reset dropped an unrelated override: maxtokens = %d", fresh.Model.MaxTokens)
	}
}

// Resetting while already on the default must not claim it changed anything.
func TestResetSilentWhenAlreadyOnDefault(t *testing.T) {
	withDefaultModel(t, "openai/chat")

	ctx := mocktest.NewMockContext().WithSystem(mocktest.NewMockSystem())
	ctx.GetConfig().Model.Model = "openai/chat"

	(&ResetCommand{}).Execute(ctx)

	if strings.Contains(ctx.LastReply(), "Model restored") {
		t.Errorf("should not mention the model when it did not change: %s", ctx.LastReply())
	}
}

// In unit tests and any path where ApplyOverrides never ran, the default is unknown.
func TestResetLeavesModelAloneWhenDefaultUnknown(t *testing.T) {
	withDefaultModel(t, "")

	ctx := mocktest.NewMockContext().WithSystem(mocktest.NewMockSystem())
	ctx.GetConfig().Model.Model = "openai/cydonia-24b"

	(&ResetCommand{}).Execute(ctx)

	if got := ctx.GetConfig().Model.Model; got != "openai/cydonia-24b" {
		t.Errorf("model must be untouched when the default is unknown, got: %s", got)
	}
}
