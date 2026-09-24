// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"B4reMetal/metald/internal/config"
)

// probeThinkingEffort asks the backend whether it will accept a reasoning effort before we commit
// to it.
func probeThinkingEffort(c *config.Configuration, effort string) (bool, error) {
	if c == nil || c.API == nil || c.Model == nil {
		return false, nil
	}
	base := strings.TrimSuffix(c.API.OpenAIURL, "/")
	if base == "" {
		return false, nil
	}

	model := c.Model.Model
	if _, name, found := strings.Cut(model, "/"); found {
		model = name
	}

	body, err := json.Marshal(map[string]any{
		"model":            model,
		"messages":         []map[string]string{{"role": "user", "content": "hi"}},
		"max_tokens":       1,
		"reasoning_effort": effort,
	})
	if err != nil {
		return false, nil
	}

	req, err := http.NewRequest("POST", base+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return false, nil
	}
	req.Header.Set("Content-Type", "application/json")
	if c.API.OpenAIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.API.OpenAIKey)
	}

	// Short timeout: this runs inside an admin command, and an unreachable backend must not
	// hang the channel.
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode < 400 {
		return true, nil
	}

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	msg := backendRefusal(raw)
	if msg == "" {
		msg = fmt.Sprintf("backend returned %d", resp.StatusCode)
	}
	return true, fmt.Errorf("the backend rejects thinkingeffort %q: %s", effort, msg)
}

// backendRefusal digs the human-readable reason out of an error envelope,
// which providers nest inconsistently.
func backendRefusal(raw []byte) string {
	var env struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &env) == nil {
		if env.Error.Message != "" {
			return firstLine(env.Error.Message)
		}
		if env.Message != "" {
			return firstLine(env.Message)
		}
	}
	return firstLine(string(raw))
}

// firstLine keeps the refusal to something an IRC line can carry.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i != -1 {
		s = s[:i]
	}
	if len(s) > 160 {
		s = s[:160] + "..."
	}
	return s
}
