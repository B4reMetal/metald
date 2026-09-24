// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"B4reMetal/metald/internal/irc"
)

const gatekeeperTimeout = 20 * time.Second

// mangling matches "do <something> to every/each <unit-of-text>".
var mangling = regexp.MustCompile(`(?i)\b(replace|swap|substitute|change|put|insert|add|remove|strip|capitali[sz]e|uppercase|lowercase|reverse|spell|separate|alternate|encode)\b[^.!?]{0,40}\b(every|each)\s+(other\s+)?(vowel|consonant|letter|char|character|word|syllable|space)s?\b`)

// ScreenIncoming reports whether a message may be answered.
func ScreenIncoming(ctx irc.ChatContextInterface, msg string) (bool, string) {
	cfg := ctx.GetConfig()
	if !isScreened(ctx, cfg.Bot.ScreenNicks) {
		return true, ""
	}
	if strings.TrimSpace(msg) == "" {
		return true, ""
	}

	// Deterministic pre-check, before the classifier is consulted at all.
	if mangling.MatchString(msg) {
		ctx.GetLogger().Info("screen_denied",
			"source", ctx.GetSource(), "reason", "text mangling (deterministic)", "message", msg)
		return false, "text mangling"
	}

	base := strings.TrimSuffix(cfg.API.OpenAIURL, "/")
	if base == "" {
		return true, ""
	}

	body, err := json.Marshal(map[string]any{
		"model": modelNameOnly(cfg.Model.Model),
		"messages": []map[string]string{
			{"role": "system", "content": cfg.Bot.GatekeeperPreamble + "\n" + cfg.Bot.GatekeeperPolicy},
			{"role": "user", "content": "--- BEGIN UNTRUSTED MESSAGE ---\n" + msg +
				"\n--- END UNTRUSTED MESSAGE ---\nVerdict:"},
		},
		"max_tokens":       64,
		"temperature":      0,
		"reasoning_effort": "none",
	})
	if err != nil {
		return true, ""
	}

	// Own context and timeout: the screen must not inherit the request's cancellation, nor hold
	// the channel for the full API timeout if the classifier hangs.
	rctx, cancel := context.WithTimeout(context.Background(), gatekeeperTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(rctx, http.MethodPost, base+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return true, ""
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.API.OpenAIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.API.OpenAIKey)
	}

	resp, err := (&http.Client{Timeout: gatekeeperTimeout}).Do(req)
	if err != nil {
		// Detail names an internal host; the channel gets nothing.
		ctx.GetLogger().Warn("screen_unavailable", "error", err.Error())
		return true, ""
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
		ctx.GetLogger().Warn("screen_unparseable", "http", resp.StatusCode)
		return true, ""
	}

	verdict := strings.TrimSpace(payload.Choices[0].Message.Content)
	if verdict == "" {
		ctx.GetLogger().Warn("screen_empty_verdict", "http", resp.StatusCode)
		return true, ""
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
		// Reason is logged, never replied: telling someone exactly which rule
		// they tripped is a free tuning signal for the next attempt.
		ctx.GetLogger().Info("screen_denied",
			"source", ctx.GetSource(), "reason", reason, "message", msg)
		return false, reason
	}

	// Prose instead of a verdict. Allowed, for the same reason as any other
	// failure: a confused classifier must not silence the channel.
	ctx.GetLogger().Warn("screen_indeterminate", "verdict", truncate(verdict, 120))
	return true, ""
}

// screened reports whether a nick is on the screening list.
func isScreened(ctx irc.ChatContextInterface, list []string) bool {
	return screened(list, ctx.GetSource())
}

func screened(list []string, nick string) bool {
	n := strings.ToLower(strings.TrimRight(nick, "_|`^"))
	for _, want := range list {
		if strings.ToLower(strings.TrimSpace(want)) == n {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// modelNameOnly strips a provider prefix ("openai/chat" -> "chat"), since the
// proxy is addressed by the bare model-group name.
func modelNameOnly(model string) string {
	if _, name, found := strings.Cut(model, "/"); found {
		return name
	}
	return model
}
