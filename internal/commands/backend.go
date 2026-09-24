// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package commands

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"time"

	"B4reMetal/metald/internal/irc"
)

// BackendCommand reports which LLM backend is actually serving requests.
type BackendCommand struct{}

func (c *BackendCommand) Name() string    { return "+backend" }
func (c *BackendCommand) AdminOnly() bool { return true }

const backendProbeTimeout = 30 * time.Second

func (c *BackendCommand) Execute(ctx irc.ChatContextInterface) {
	cfg := ctx.GetConfig()
	base := strings.TrimSuffix(cfg.API.OpenAIURL, "/")
	if base == "" {
		ctx.Reply("No openaiurl configured")
		return
	}

	body := []byte(`{"model":"` + modelNameOnly(cfg.Model.Model) +
		`","messages":[{"role":"user","content":"hi"}],"max_tokens":400}`)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		base+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		ctx.Reply(fmt.Sprintf("Failed to build probe: %v", err))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.API.OpenAIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.API.OpenAIKey)
	}

	client := &http.Client{Timeout: backendProbeTimeout}
	resp, err := client.Do(req)
	if err != nil {
		// Detail (url, dial error) goes to the log only - it names internal
		// hosts and ports, and this reply lands in a public channel.
		ctx.GetLogger().Warn("backend_probe_failed", "url", base, "error", err.Error())
		ctx.Reply("Backend unreachable - see logs")
		return
	}
	defer resp.Body.Close()

	group := resp.Header.Get("x-litellm-model-group")
	modelID := resp.Header.Get("x-litellm-model-id")
	apiBase := resp.Header.Get("x-litellm-model-api-base")

	// api_base is deliberately logged but never replied: it's an internal address.
	ctx.GetLogger().Info("backend_probe",
		"group", group, "model_id", modelID, "api_base", apiBase, "http", resp.StatusCode)

	if modelID == "" && group == "" {
		// Not behind LiteLLM (or an older version) - report what we can
		// rather than pretending to know which deployment answered.
		ctx.Reply(fmt.Sprintf("%s (no proxy routing headers; http %d)",
			cfg.Model.Model, resp.StatusCode))
		return
	}

	status := "primary"
	if strings.HasSuffix(group, "-backup") {
		status = "FALLBACK - primary is down"
	}

	ctx.Reply(fmt.Sprintf("%s (%s) - %s", modelID, group, status))
}

// modelNameOnly strips a leading provider prefix ("openai/chat" -> "chat"),
// since the proxy is addressed by the bare model-group name.
func modelNameOnly(model string) string {
	if _, name, found := strings.Cut(model, "/"); found {
		return name
	}
	return model
}
