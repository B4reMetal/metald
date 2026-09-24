// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package commands

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mocktest "B4reMetal/metald/internal/testing"
)

const modelListJSON = `{"data":[
  {"id":"chat"},{"id":"chat-backup"},
  {"id":"vision"},{"id":"vision-backup"},
  {"id":"qwen3-coder-30b"},{"id":"qwen3-8b"}
]}`

// runModels points the command at a stub /models endpoint and returns the
// reply plus the context, so tests can assert on config changes too.
func runModels(t *testing.T, args ...string) (string, *mocktest.MockChatContext) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/models") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write([]byte(modelListJSON))
	}))
	defer srv.Close()

	ctx := mocktest.NewMockContext().
		WithAdmin(true).
		WithSystem(mocktest.NewMockSystem())
	cfg := ctx.GetConfig()
	cfg.API.OpenAIURL = srv.URL
	cfg.Model.Model = "openai/chat"
	ctx.WithArgs(append([]string{"+models"}, args...)...)

	(&ModelsCommand{}).Execute(ctx)
	if len(ctx.Replies) != 1 {
		t.Fatalf("expected 1 reply, got %d: %v", len(ctx.Replies), ctx.Replies)
	}
	return ctx.Replies[0], ctx
}

func TestModelsListsAndMarksCurrent(t *testing.T) {
	reply, _ := runModels(t)
	if !strings.Contains(reply, "[chat]") {
		t.Errorf("current model should be bracketed, got: %s", reply)
	}
	if !strings.Contains(reply, "qwen3-coder-30b") {
		t.Errorf("selectable model missing, got: %s", reply)
	}
}

// The -backup groups are failover targets.
func TestModelsHidesFailoverGroups(t *testing.T) {
	reply, _ := runModels(t)
	for _, hidden := range []string{"chat-backup", "vision-backup"} {
		if strings.Contains(reply, hidden) {
			t.Errorf("%s should not be offered for selection: %s", hidden, reply)
		}
	}
}

func TestModelsSwitchesToValidModel(t *testing.T) {
	reply, ctx := runModels(t, "qwen3-coder-30b")
	if !strings.Contains(reply, "qwen3-coder-30b") {
		t.Errorf("expected confirmation, got: %s", reply)
	}
	if got := ctx.GetConfig().Model.Model; got != "openai/qwen3-coder-30b" {
		t.Errorf("provider prefix should be preserved, got: %s", got)
	}
}

// The whole point of this command over "+set model": a name the proxy does not
// serve must not be accepted, persisted, and left to fail every completion.
func TestModelsRejectsUnknownModel(t *testing.T) {
	reply, ctx := runModels(t, "gpt-5-turbo-ultra")
	if !strings.Contains(reply, "no such model") {
		t.Errorf("expected a rejection, got: %s", reply)
	}
	if got := ctx.GetConfig().Model.Model; got != "openai/chat" {
		t.Errorf("model must be unchanged after a rejected switch, got: %s", got)
	}
}

func TestModelsAcceptsCaseInsensitivelyButStoresCanonical(t *testing.T) {
	_, ctx := runModels(t, "QWEN3-Coder-30B")
	if got := ctx.GetConfig().Model.Model; got != "openai/qwen3-coder-30b" {
		t.Errorf("should persist the proxy's own spelling, got: %s", got)
	}
}

// Accepts a fully-qualified name too, rather than rejecting it as unknown.
func TestModelsAcceptsProviderPrefixedArgument(t *testing.T) {
	_, ctx := runModels(t, "openai/qwen3-8b")
	if got := ctx.GetConfig().Model.Model; got != "openai/qwen3-8b" {
		t.Errorf("expected the prefixed form to resolve, got: %s", got)
	}
}

func TestModelsIsAdminOnly(t *testing.T) {
	if !(&ModelsCommand{}).AdminOnly() {
		t.Error("+models changes the model and evicts the GPU; it must be admin-only")
	}
}

func TestModelsLeaksNoInternalAddressOnFailure(t *testing.T) {
	ctx := mocktest.NewMockContext()
	// Port 1 refuses instantly; an unrouted address would hang to the deadline.
	ctx.GetConfig().API.OpenAIURL = "http://127.0.0.1:1/v1"
	ctx.GetConfig().Model.Model = "openai/chat"
	ctx.WithArgs("+models")

	(&ModelsCommand{}).Execute(ctx)

	for _, r := range ctx.Replies {
		if strings.Contains(r, "127.0.0.1") || strings.Contains(r, "/v1") {
			t.Errorf("reply leaked an internal address: %s", r)
		}
	}
}
