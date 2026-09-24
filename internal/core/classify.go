// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

const classifyTimeout = 20 * time.Second

func Classify(ctx ChatContextInterface, policy, label, content string, failOpen bool) (bool, string) {
	cfg := ctx.GetConfig()
	base := strings.TrimSuffix(cfg.API.OpenAIURL, "/")
	if base == "" || strings.TrimSpace(content) == "" {
		return failOpen, ""
	}

	model := cfg.Model.Model
	if _, name, found := strings.Cut(model, "/"); found {
		model = name
	}

	body, err := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": cfg.Bot.ClassifyPreamble + "\n" + policy},
			{"role": "user", "content": "--- BEGIN UNTRUSTED " + label + " ---\n" + content +
				"\n--- END UNTRUSTED " + label + " ---\nVerdict:"},
		},
		"max_tokens":       64,
		"temperature":      0,
		"reasoning_effort": "none",
	})
	if err != nil {
		return failOpen, ""
	}

	// Its own context and timeout: a review must not inherit a cancellation from the request being
	// reviewed, nor hold the channel up for the full API timeout if the classifier hangs.
	rctx, cancel := context.WithTimeout(context.Background(), classifyTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(rctx, http.MethodPost, base+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return failOpen, ""
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.API.OpenAIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.API.OpenAIKey)
	}

	resp, err := (&http.Client{Timeout: classifyTimeout}).Do(req)
	if err != nil {
		// Detail names an internal host; the channel never sees it.
		ctx.GetLogger().Warn("classify_unavailable", "label", label, "error", err.Error())
		return failOpen, "safety check unavailable"
	}
	defer resp.Body.Close()

	var payload struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil || len(payload.Choices) == 0 {
		ctx.GetLogger().Warn("classify_unparseable", "label", label, "http", resp.StatusCode)
		return failOpen, "safety check inconclusive"
	}

	verdict := strings.TrimSpace(payload.Choices[0].Message.Content)
	if verdict == "" {
		ctx.GetLogger().Warn("classify_empty_verdict", "label", label, "http", resp.StatusCode)
		return failOpen, "safety check inconclusive"
	}

	first := strings.ToUpper(strings.TrimSpace(strings.SplitN(verdict, "\n", 2)[0]))
	if first == "ALLOW" || strings.HasPrefix(first, "ALLOW ") {
		return true, ""
	}
	if strings.HasPrefix(first, "DENY") {
		reason := strings.TrimSpace(strings.TrimLeft(first[4:], ": "))
		if reason == "" {
			reason = "unspecified"
		}
		if len(reason) > 60 {
			reason = reason[:60]
		}
		return false, reason
	}

	ctx.GetLogger().Warn("classify_indeterminate", "label", label, "verdict", truncate(verdict, 120))
	return failOpen, "safety check inconclusive"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
