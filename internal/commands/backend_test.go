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

// runBackend points the command at a stub server and returns the bot's reply.
func runBackend(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(handler)
	defer srv.Close()

	ctx := mocktest.NewMockContext()
	cfg := ctx.GetConfig()
	cfg.API.OpenAIURL = srv.URL
	cfg.Model.Model = "openai/chat"

	(&BackendCommand{}).Execute(ctx)
	if len(ctx.Replies) != 1 {
		t.Fatalf("expected 1 reply, got %d: %v", len(ctx.Replies), ctx.Replies)
	}
	return ctx.Replies[0]
}

func TestBackendReportsPrimary(t *testing.T) {
	reply := runBackend(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-litellm-model-group", "chat")
		w.Header().Set("x-litellm-model-id", "remote-vllm-qwen38")
		w.Header().Set("x-litellm-model-api-base", "http://192.168.99.153:18020/v1")
		w.Write([]byte(`{"choices":[{"message":{"content":"hi"}}]}`))
	})
	if !strings.Contains(reply, "remote-vllm-qwen38") || !strings.Contains(reply, "primary") {
		t.Fatalf("expected a primary report, got: %s", reply)
	}
	if strings.Contains(reply, "FALLBACK") {
		t.Fatalf("primary must not be reported as fallback: %s", reply)
	}
}

// The whole point of the command: making a silent degradation visible.
func TestBackendReportsFallback(t *testing.T) {
	reply := runBackend(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-litellm-model-group", "chat-backup")
		w.Header().Set("x-litellm-model-id", "local-llamacpp-qwen36")
		w.Header().Set("x-litellm-model-api-base", "http://127.0.0.1:8080/v1")
		w.Write([]byte(`{"choices":[{"message":{"content":"hi"}}]}`))
	})
	if !strings.Contains(reply, "FALLBACK") {
		t.Fatalf("expected a fallback warning, got: %s", reply)
	}
}

// Pointing straight at a backend (no proxy) must degrade gracefully rather
// than claiming to know which deployment answered.
func TestBackendWithoutProxyHeaders(t *testing.T) {
	reply := runBackend(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[{"message":{"content":"hi"}}]}`))
	})
	if !strings.Contains(reply, "no proxy routing headers") {
		t.Fatalf("expected a graceful no-headers reply, got: %s", reply)
	}
}

// The reply lands in a public channel, so it must never contain internal
// addresses - not the backend's api_base, and not the configured proxy URL.
func TestBackendDoesNotLeakInternalAddresses(t *testing.T) {
	reply := runBackend(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-litellm-model-group", "chat")
		w.Header().Set("x-litellm-model-id", "remote-vllm-qwen38")
		w.Header().Set("x-litellm-model-api-base", "http://192.168.99.153:18020/v1")
		w.Write([]byte(`{"choices":[{"message":{"content":"hi"}}]}`))
	})
	for _, leak := range []string{"192.168.", "18020", "127.0.0.1", "http://"} {
		if strings.Contains(reply, leak) {
			t.Fatalf("reply leaked %q: %s", leak, reply)
		}
	}
	// It must still be useful - the deployment id identifies the backend.
	if !strings.Contains(reply, "remote-vllm-qwen38") {
		t.Fatalf("reply should still name the deployment: %s", reply)
	}
}

func TestBackendUnreachable(t *testing.T) {
	ctx := mocktest.NewMockContext()
	cfg := ctx.GetConfig()
	// Nothing listens here; the probe must report it instead of hanging or panicking.
	cfg.API.OpenAIURL = "http://127.0.0.1:1" // nothing listens here
	cfg.Model.Model = "openai/chat"

	(&BackendCommand{}).Execute(ctx)
	if len(ctx.Replies) != 1 || !strings.Contains(ctx.Replies[0], "unreachable") {
		t.Fatalf("expected an unreachable report, got: %v", ctx.Replies)
	}
}

// Diagnostics must not queue behind the request they're diagnosing.
func TestBackendBypassesLock(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&BackendCommand{})
	if !registry.BypassesLock("+backend") {
		t.Fatal("+backend should bypass the request lock")
	}
}

func TestBackendIsAdminOnly(t *testing.T) {
	if !(&BackendCommand{}).AdminOnly() {
		t.Fatal("+backend exposes internal api_base addresses and must be admin-only")
	}
}

func TestModelNameOnly(t *testing.T) {
	cases := map[string]string{
		"openai/chat":        "chat",
		"chat":               "chat",
		"openai/qwen3.8-27b": "qwen3.8-27b",
	}
	for in, want := range cases {
		if got := modelNameOnly(in); got != want {
			t.Errorf("modelNameOnly(%q) = %q, want %q", in, got, want)
		}
	}
}
