// Copyright (C) 2023-2026 Alex Schlessinger and soulshack contributors
// Modified 2026 by BareMetal
// SPDX-License-Identifier: GPL-3.0-only

package bot

import (
	"log/slog"
	"os"
	"slices"
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
	s.Tools.RegisterNative("task__delegate", llm.NewDelegateTool)

	// Load all tools from configuration (polly now handles native, shell, and MCP tools)
	adminTools := make(map[string]bool, len(c.Bot.AdminTools))
	for _, name := range c.Bot.AdminTools {
		adminTools[name] = true
	}

	// Tools are optional, but an enabled tool must be loadable and have every
	// credential it declares, or the bot does not start.
	unusable := 0
	if len(c.Bot.Tools) > 0 {
		for _, toolSpec := range withTaskToolset(c.Bot.Tools) {
			result, err := s.Tools.LoadToolAuto(toolSpec)
			if err != nil {
				slog.Error("tool_load_failed", "tool", toolSpec, "error", err)
				unusable++
				continue
			}
			if result.Type == "shell" {
				meta, err := core.ReadShellToolMeta(toolSpec)
				req := meta.Requires
				if err != nil {
					slog.Error("tool_requirements_unreadable", "tool", toolSpec, "error", err)
					unusable++
					continue
				}
				if missing := core.MissingEnv(req, nil); len(missing) > 0 {
					slog.Error("tool_requirements_missing", "tool", toolSpec,
						"keys", strings.Join(missing, ", "), "hint", "set them under env: in config.yml, or remove the tool")
					unusable++
					continue
				}
			}

			if result.Type == "shell" {
				if meta, err := core.ReadShellToolMeta(toolSpec); err == nil {
					for _, server := range result.Servers {
						for _, name := range server.ToolNames {
							if meta.Announce != nil && !*meta.Announce {
								core.SetQuietTool(name)
							}
							if tool, ok := s.Tools.Get(name); ok && meta.Requester {
								s.Tools.Register(irc.NewRequesterTool(tool))
							}
						}
					}
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

	// Conversations are saved to the context database, so a restart resumes them.
	db, err := core.Context()
	if err != nil {
		slog.Error("context_db_unavailable", "error", err.Error())
		os.Exit(1)
	}
	s.Store = core.NewPersistentSessionStore(db, &sessions.Metadata{SystemPrompt: c.Bot.Prompt})

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

// withTaskToolset adds the rest of the background-work tools when task__start is enabled: they only
// make sense together, and the ones used inside a task are hidden from chat anyway.
func withTaskToolset(specs []string) []string {
	if !slices.Contains(specs, "task__start") {
		return specs
	}
	out := slices.Clone(specs)
	for _, name := range irc.TaskToolset {
		if !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}
