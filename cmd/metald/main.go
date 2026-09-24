// Copyright (C) 2023-2026 Alex Schlessinger and soulshack contributors
// Modified 2026 by BareMetal
// SPDX-License-Identifier: GPL-3.0-only

package main

//  ____                    _   ____    _                      _
// / ___|    ___    _   _  | | / ___|  | |__     __ _    ___  | | __
// \___ \   / _ \  | | | | | | \___ \  | '_ \   / _` |  / __| | |/ /
//  ___) | | (_) | | |_| | | |  ___) | | | | | | (_| | | (__  |   <
// |____/   \___/   \__,_| |_| |____/  |_| |_|  \__,_|  \___| |_|\_\
//  .  .  .  because  real  people  are  overrated

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/urfave/cli/v3"

	"B4reMetal/metald/internal/bot"
	"B4reMetal/metald/internal/config"
)

func main() {
	fmt.Printf("%s\n", bot.GetBanner())

	// Create a context that cancels on SIGINT or SIGTERM
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cmd := &cli.Command{
		Name:    "metald",
		Usage:   "because real people are overrated",
		Version: bot.Version + " - https://github.com/B4reMetal/metald",
		Flags:   config.GetFlags(),
		Action: func(_ context.Context, c *cli.Command) error {
			if len(os.Args) == 1 && c.String("config") == "" {
				return cli.ShowAppHelp(c)
			}
			// Use our cancellable context, not the CLI's context
			return bot.Run(ctx, config.NewConfiguration(c))
		},
	}

	if err := cmd.Run(ctx, os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
