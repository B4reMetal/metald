// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package commands

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"B4reMetal/metald/internal/irc"
)

// ModelsCommand lists the models the proxy can serve, and switches between them.
type ModelsCommand struct{}

func (c *ModelsCommand) Name() string    { return "+models" }
func (c *ModelsCommand) AdminOnly() bool { return true }

const modelsProbeTimeout = 15 * time.Second

func (c *ModelsCommand) Execute(ctx irc.ChatContextInterface) {
	cfg := ctx.GetConfig()
	base := strings.TrimSuffix(cfg.API.OpenAIURL, "/")
	if base == "" {
		ctx.Reply("No openaiurl configured")
		return
	}

	available, err := fetchModels(ctx, base, cfg.API.OpenAIKey)
	if err != nil {
		// The URL names an internal host; the channel gets none of it.
		ctx.GetLogger().Warn("models_probe_failed", "url", base, "error", err.Error())
		ctx.Reply("Could not reach the model list - see logs")
		return
	}
	if len(available) == 0 {
		ctx.Reply("The proxy reports no models")
		return
	}

	current := modelNameOnly(cfg.Model.Model)

	// No argument: list what's on offer and mark the active one.
	if len(ctx.GetArgs()) < 2 {
		var b strings.Builder
		for i, m := range available {
			if i > 0 {
				b.WriteString(", ")
			}
			if m == current {
				fmt.Fprintf(&b, "[%s]", m)
			} else {
				b.WriteString(m)
			}
		}
		ctx.Reply(fmt.Sprintf("models: %s  (current in brackets; +models <name> to switch)", b.String()))
		return
	}

	want := modelNameOnly(strings.TrimSpace(ctx.GetArgs()[1]))

	// Match case-insensitively but switch to the proxy's own spelling, so the
	// persisted value is always exactly what the proxy answers to.
	canonical := ""
	for _, m := range available {
		if strings.EqualFold(m, want) {
			canonical = m
			break
		}
	}
	if canonical == "" {
		ctx.GetLogger().Info("model_switch_rejected", "requested", want, "by", ctx.GetSource())
		ctx.Reply(fmt.Sprintf("no such model: %s. available: %s", want, strings.Join(available, ", ")))
		return
	}

	if canonical == current {
		ctx.Reply(fmt.Sprintf("already on %s", canonical))
		return
	}

	// Keep the provider prefix the existing config uses ("openai/chat"), since
	// the LLM client routes on it.
	value := canonical
	if prefix, _, found := strings.Cut(cfg.Model.Model, "/"); found {
		value = prefix + "/" + canonical
	}

	field := configFields["model"]
	if err := field.setter(cfg, value); err != nil {
		ctx.Reply(err.Error())
		return
	}
	if err := ctx.GetSystem().UpdateLLM(*cfg.API); err != nil {
		ctx.GetLogger().Error("llm_update_failed", "error", err)
		ctx.Reply("Model saved, but failed to update the LLM client")
		return
	}
	PersistSet("model", value)

	ctx.GetSession().Clear()

	ctx.GetLogger().Info("model_switched", "from", current, "to", canonical, "by", ctx.GetSource())
	ctx.Reply(fmt.Sprintf("model set to %s (history cleared; first reply may be slow while it loads)", canonical))
}

// fetchModels asks the proxy which model groups it serves.
func fetchModels(ctx irc.ChatContextInterface, base, key string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models", nil)
	if err != nil {
		return nil, err
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	resp, err := (&http.Client{Timeout: modelsProbeTimeout}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("model list returned http %d", resp.StatusCode)
	}

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	var out []string
	for _, m := range payload.Data {
		// The "-backup" groups are failover targets, not things to select by hand: picking one pins the
		// bot to the weak local model with no way back if the remote recovers.
		if m.ID == "" || strings.HasSuffix(m.ID, "-backup") || strings.HasSuffix(m.ID, "-fallback") {
			continue
		}
		out = append(out, m.ID)
	}
	sort.Strings(out)
	return out, nil
}
