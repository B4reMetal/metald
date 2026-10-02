// Copyright (C) 2023-2026 Alex Schlessinger and soulshack contributors
// Modified 2026 by BareMetal
// SPDX-License-Identifier: GPL-3.0-only

package bot

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"sync"
	"time"

	"github.com/lrstanley/girc"

	"B4reMetal/metald/internal/admin"
	"B4reMetal/metald/internal/behaviors"
	"B4reMetal/metald/internal/commands"
	"B4reMetal/metald/internal/config"
	"B4reMetal/metald/internal/core"
	"B4reMetal/metald/internal/irc"
	"B4reMetal/metald/internal/llm"
)

// Run starts the IRC bot with the given configuration
func Run(ctx context.Context, cfg *config.Configuration) error {
	level := cfg.Bot.LogLevel
	if cfg.Bot.Verbose {
		level = "debug"
	}
	core.InitLogger(level, cfg.Bot.LogFormat)

	if err := irc.ValidCommandPrefix(cfg.Bot.CommandPrefix); err != nil {
		return fmt.Errorf("commandprefix %q: %w", cfg.Bot.CommandPrefix, err)
	}
	core.SetDataDir(cfg.Bot.DataDir)
	commands.OverridesPath = core.DataPath("config-overrides.json")

	// Layer persisted runtime changes (+set, +admins, +tools) over config.yml. Must run
	// before NewSystem, which reads them.
	commands.ApplyOverrides(cfg)
	core.SetConcurrency(cfg.Bot.MaxConcurrent)
	if lib := core.ExportPluginLib(cfg.Bot.PluginLib); lib != "" {
		core.GetLogger().Info("plugin_lib_exported", "path", lib)
	} else {
		core.GetLogger().Warn("plugin_lib_missing", "path", cfg.Bot.PluginLib)
	}

	sys := NewSystem(cfg)

	// Write changed conversations to disk until shutdown, then once more before returning.
	if ps, ok := sys.GetSessionStore().(*core.PersistentSessionStore); ok {
		flushCtx, stopFlush := context.WithCancel(ctx)
		flushed := make(chan struct{})
		go func() { ps.Run(flushCtx); close(flushed) }()
		defer func() { stopFlush(); <-flushed }()
	}
	go llm.RunChatLogPruner(ctx, cfg.Session.HistoryDays)

	// Initialize command registry
	cmdRegistry := commands.NewRegistry()
	cmdRegistry.Register(&commands.SetCommand{})
	cmdRegistry.Register(&commands.GetCommand{})
	cmdRegistry.Register(commands.NewHelpCommand(cmdRegistry))
	cmdRegistry.Register(&commands.VersionCommand{Version: "v" + Version})
	cmdRegistry.Register(&commands.CompletionCommand{})
	cmdRegistry.Register(&commands.ToolsCommand{})
	cmdRegistry.Register(&commands.AdminCommand{})
	cmdRegistry.Register(&commands.StatsCommand{})
	cmdRegistry.Register(&commands.IgnoreCommand{})
	cmdRegistry.Register(&commands.ScreenCommand{})
	cmdRegistry.Register(&commands.UnscreenCommand{})
	cmdRegistry.Register(&commands.UnignoreCommand{})
	cmdRegistry.Register(&commands.ResetCommand{})
	cmdRegistry.Register(&commands.BackendCommand{})
	cmdRegistry.Register(&commands.ModelsCommand{})
	cmdRegistry.Register(&commands.PromptCommand{})
	cmdRegistry.Register(&commands.MemoriesCommand{})
	cmdRegistry.Register(&commands.RecapCommand{})
	cmdRegistry.Register(&commands.SuspicionCommand{})
	cmdRegistry.Register(&commands.NowPlayingCommand{})
	cmdRegistry.Register(&commands.SkipCommand{})
	cmdRegistry.Register(&commands.TaskCommand{})
	cmdRegistry.Register(&commands.TasksCommand{})
	cmdRegistry.Register(&commands.GoalCommand{})
	cmdRegistry.Register(&commands.ScheduleCommand{})

	// Initialize behavior registry (order matters: passive watchers first, addressed last as fallback)
	behaviorRegistry := behaviors.NewRegistry()
	// Lifecycle behaviors
	behaviorRegistry.Register(&behaviors.ConnectedBehavior{})
	behaviorRegistry.Register(&behaviors.NickErrorBehavior{})
	behaviorRegistry.Register(&behaviors.ChannelErrorBehavior{})
	behaviorRegistry.Register(&behaviors.SendErrorBehavior{})
	// Reactive behaviors
	behaviorRegistry.Register(&behaviors.URLBehavior{})
	behaviorRegistry.Register(&behaviors.OpBehavior{})
	behaviorRegistry.Register(&behaviors.JoinBehavior{})
	behaviorRegistry.Register(&behaviors.AddressedBehavior{CmdRegistry: cmdRegistry})
	behaviorRegistry.Register(&behaviors.NonAddressedBehavior{CmdRegistry: cmdRegistry})

	nets := cfg.Networks
	if len(nets) == 0 {
		nets = []*config.ServerConfig{cfg.Server}
	}

	startAdmin(ctx, cfg, nets)

	// Memories written before this bot knew about networks carry no network of their own.
	if len(nets) > 0 && nets[0].Name != "" {
		if store, err := core.Memories(); err == nil {
			if n, err := store.AdoptUnscopedMemories(nets[0].Name); err != nil {
				slog.Error("memory_migration_failed", "error", err.Error())
			} else if n > 0 {
				slog.Info("memories_assigned_to_network", "network", nets[0].Name, "count", n)
			}
		}
		if n := core.Reminders().AdoptUnscopedReminders(nets[0].Name); n > 0 {
			slog.Info("reminders_assigned_to_network", "network", nets[0].Name, "count", n)
		}
	}

	if len(nets) == 1 {
		return runNetwork(ctx, cfg.ForNetwork(nets[0]), sys, behaviorRegistry, cmdRegistry)
	}

	errs := make(chan error, len(nets))
	var wg sync.WaitGroup
	for _, n := range nets {
		wg.Add(1)
		go func(n *config.ServerConfig) {
			defer wg.Done()
			netCfg := cfg.ForNetwork(n)
			if err := runNetwork(ctx, netCfg, sys, behaviorRegistry, cmdRegistry); err != nil {
				slog.Error("network_failed", "network", n.Name, "error", err.Error())
				errs <- fmt.Errorf("network %s: %w", n.Name, err)
			}
		}(n)
	}
	wg.Wait()
	close(errs)

	if err, ok := <-errs; ok {
		return err
	}
	return nil
}

// runNetwork connects one network and serves it until the context ends.
func runNetwork(ctx context.Context, cfg *config.Configuration, sys core.System, behaviorRegistry *behaviors.Registry, cmdRegistry *commands.Registry) error {
	// Channel for fatal IRC errors (nick taken, channel join failures)
	fatalErr := make(chan error, 1)

	ircClient := girc.New(girc.Config{
		Server:     cfg.Server.Server,
		Port:       cfg.Server.Port,
		Nick:       cfg.Server.Nick,
		User:       cfg.Server.Nick,
		Name:       cfg.Server.Nick,
		Version:    "irc client",
		ServerPass: cfg.Server.ServerPass,
		SSL:        cfg.Server.SSL,
		TLSConfig: &tls.Config{
			ServerName:         cfg.Server.Server,
			InsecureSkipVerify: cfg.Server.TLSInsecure,
		},
		HandleNickCollide: func(oldNick string) string {
			return "" // Don't auto-retry, we handle it via ERR_NICKNAMEINUSE
		},
	})

	log := slog.Default()
	if cfg.Server.Name != "" {
		log = log.With("network", cfg.Server.Name)
	}

	if cfg.Server.SASLNick != "" && cfg.Server.SASLPass != "" {
		ircClient.Config.SASL = &girc.SASLPlain{
			User: cfg.Server.SASLNick,
			Pass: cfg.Server.SASLPass,
		}
	}

	go func() {
		<-ctx.Done()
		ircClient.Quit("Shutting down...")
		log.Info("irc_client_closed")
	}()

	// Idle conversations on this network are folded into their channel recap.
	go llm.RunIdleFolder(ctx, cfg, sys.GetSessionStore())

	// Background tasks on this network run one at a time, posting results where they were asked for.
	go llm.RunTaskRunner(ctx, cfg, sys, ircClient, fatalErr)

	// Reminders fire outside any request context, so delivery lives here rather than in the tool that
	// schedules them.
	go core.RunReminderScheduler(ctx, core.Reminders(), cfg.Server.Name, func(channel, message string) bool {
		if !ircClient.IsConnected() {
			return false
		}
		message = irc.RenderIRCFormatting(message)
		if prefix := cfg.EffectiveResponsePrefix(); prefix != "" {
			message = prefix + " " + message
		}
		ircClient.Cmd.Message(channel, message)
		return true
	})

	// Single global handler routes all events through the behavior registry
	ircClient.Handlers.AddBg(girc.ALL_EVENTS, func(client *girc.Client, e girc.Event) {
		if !behaviorRegistry.Handles(e.Command) {
			return
		}
		chatCtx, cancel := irc.NewChatContext(ctx, cfg, sys, client, &e, fatalErr)
		defer cancel()

		// Register the request so "+reset" and a mid-flight ignore can actually stop it, rather than
		// letting it finish and reply to someone who is no longer entitled to an answer.
		if e.Source != nil {
			done := core.Requests().Track(chatCtx.GetLockKey(), chatCtx.GetRequestID(), e.Source.Name, cancel)
			defer done()
		}
		behaviors.ObserveLine(chatCtx, &e, cmdRegistry)
		behaviorRegistry.Process(chatCtx, &e)
	})

	// Reconnect loop
	const maxRetries = 5
	for i := range maxRetries {
		if ctx.Err() != nil {
			return nil
		}

		log.Info("server_connecting",
			"server", ircClient.Config.Server,
			"port", ircClient.Config.Port,
			"tls", ircClient.Config.SSL,
			"sasl", ircClient.Config.SASL != nil,
		)

		if err := ircClient.Connect(); err != nil {
			if ctx.Err() != nil {
				return nil
			}

			// Check for fatal IRC errors (nick taken, channel join failures)
			select {
			case fErr := <-fatalErr:
				return fErr
			default:
			}

			log.Error("connection_failed", "error", err)
			log.Info("connection_retry", "delay_sec", 5, "attempt", i+1, "max_attempts", maxRetries)

			select {
			case <-time.After(5 * time.Second):
				continue
			case <-ctx.Done():
				return nil
			}
		}

		// Check for fatal IRC errors after successful connection closed
		select {
		case fErr := <-fatalErr:
			return fErr
		default:
		}

		return nil
	}

	return fmt.Errorf("failed to connect after %d attempts", maxRetries)
}

// startAdmin serves the operator page when it is configured; without a token it stays off.
func startAdmin(ctx context.Context, cfg *config.Configuration, nets []*config.ServerConfig) {
	if cfg.Bot.AdminListen == "" {
		return
	}
	if cfg.Bot.AdminToken == "" {
		core.GetLogger().Warn("admin_disabled", "reason", "adminlisten is set but admintoken is empty")
		return
	}
	names := make([]string, 0, len(nets))
	for _, n := range nets {
		names = append(names, n.Name)
	}
	var proxies []netip.Prefix
	for _, p := range cfg.Bot.AdminTrustedProxies {
		prefix, err := netip.ParsePrefix(p)
		if addr, aerr := netip.ParseAddr(p); aerr == nil {
			prefix, err = addr.Prefix(addr.BitLen())
		}
		if err != nil {
			core.GetLogger().Error("admin_bad_proxy", "proxy", p, "error", err.Error())
			continue
		}
		proxies = append(proxies, prefix)
	}
	srv, err := admin.NewFromCore(admin.Config{Token: cfg.Bot.AdminToken, Version: "v" + Version, Networks: names,
		Started: time.Now(), ComfyURL: os.Getenv("COMFYUI_URL"), RadioURL: os.Getenv("RADIO_API_URL"),
		TrustedProxies: proxies, UserHeader: cfg.Bot.AdminUserHeader, Users: cfg.Bot.AdminUsers}, core.GetLogger())
	if err != nil {
		core.GetLogger().Error("admin_failed", "error", err.Error())
		return
	}
	go func() {
		core.GetLogger().Info("admin_listening", "addr", cfg.Bot.AdminListen)
		if err := srv.Run(ctx, cfg.Bot.AdminListen); err != nil {
			core.GetLogger().Error("admin_failed", "error", err.Error())
		}
	}()
}
