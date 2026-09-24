// Copyright (C) 2023-2026 Alex Schlessinger and soulshack contributors
// Modified 2026 by BareMetal
// SPDX-License-Identifier: GPL-3.0-only

package bot

import (
	"log/slog"
	"os"
	"strings"
	"sync/atomic"

	"github.com/alexschlessinger/pollytool/sessions"
	"github.com/alexschlessinger/pollytool/tools"
	"github.com/alexschlessinger/pollytool/tools/sandbox"

	"B4reMetal/metald/internal/config"
	"B4reMetal/metald/internal/core"
	"B4reMetal/metald/internal/irc"
	"B4reMetal/metald/internal/llm"
)

type SystemImpl struct {
	Store sessions.SessionStore
	Tools *tools.ToolRegistry
	llm   atomic.Value // stores core.LLM
}

func (s *SystemImpl) GetToolRegistry() *tools.ToolRegistry {
	return s.Tools
}

func (s *SystemImpl) GetSessionStore() sessions.SessionStore {
	return s.Store
}

func (s *SystemImpl) GetLLM() core.LLM {
	return s.llm.Load().(core.LLM)
}

func (s *SystemImpl) UpdateLLM(cfg config.APIConfig) error {
	slog.Info("llm_updating")
	s.llm.Store(llm.NewPollyLLM(cfg))
	return nil
}

func NewSystem(c *config.Configuration) core.System {
	s := &SystemImpl{}

	// Optionally enable platform sandboxing for shell/bash/MCP tools.
	var regOpts []tools.RegistryOption
	if c.Bot.Sandbox {
		baseCfg := sandbox.DefaultConfig()
		if _, err := sandbox.New(baseCfg); err != nil {
			slog.Warn("sandbox_unavailable", "error", err)
		} else {
			regOpts = append(regOpts, tools.WithSandboxFactory(sandbox.New, baseCfg))
			slog.Info("sandbox_enabled")
		}
	}
	s.Tools = tools.NewToolRegistry([]tools.Tool{}, regOpts...)

	// Register native IRC tools with polly's registry
	irc.RegisterIRCTools(s.Tools)

	// Load all tools from configuration (polly now handles native, shell, and MCP tools)
	adminTools := make(map[string]bool, len(c.Bot.AdminTools))
	for _, name := range c.Bot.AdminTools {
		adminTools[name] = true
	}

	// Tools are optional, but an enabled tool must be loadable and have every
	// credential it declares, or the bot does not start.
	unusable := 0
	if len(c.Bot.Tools) > 0 {
		for _, toolSpec := range c.Bot.Tools {
			result, err := s.Tools.LoadToolAuto(toolSpec)
			if err != nil {
				slog.Error("tool_load_failed", "tool", toolSpec, "error", err)
				unusable++
				continue
			}
			if result.Type == "shell" {
				req, err := core.ShellToolRequirements(toolSpec)
				if err != nil {
					slog.Error("tool_requirements_unreadable", "tool", toolSpec, "error", err)
					unusable++
					continue
				}
				if missing := core.MissingEnv(req, nil); len(missing) > 0 {
					slog.Error("tool_requirements_missing", "tool", toolSpec,
						"keys", strings.Join(missing, ", "), "hint", "set them in .env or remove the tool")
					unusable++
					continue
				}
			}

			// Re-wrap any tool this config restricted to admins.
			for _, server := range result.Servers {
				for _, toolName := range server.ToolNames {
					if !adminTools[toolName] {
						continue
					}
					tool, ok := s.Tools.Get(toolName)
					if !ok {
						continue
					}
					s.Tools.Register(irc.NewAdminOnlyTool(tool))
					slog.Debug("tool_restricted_to_admins", "tool_name", toolName)
				}
			}
		}
	}

	if unusable > 0 {
		slog.Error("tools_unusable", "count", unusable, "hint", "fix the tool or remove it from the tool list")
		os.Exit(1)
	}

	// initialize sessions with pollytool's SyncMapSessionStore
	s.Store = sessions.NewSyncMapSessionStore(&sessions.Metadata{
		MaxHistoryTokens: c.Session.MaxContext,
		TTL:              c.Session.TTL,
		SystemPrompt:     c.Bot.Prompt,
	})

	// Initialize LLM
	s.UpdateLLM(*c.API)

	// Log startup summary
	fields := []any{
		"model", c.Model.Model,
		"tools_loaded", len(s.Tools.All()),
		"max_context", c.Session.MaxContext,
	}
	slog.Info("system_initialized", fields...)

	return s
}
