// Copyright (C) 2023-2026 Alex Schlessinger and soulshack contributors
// Modified 2026 by BareMetal
// SPDX-License-Identifier: GPL-3.0-only

package commands

import (
	"regexp"
	"sort"
	"strings"

	"B4reMetal/metald/internal/irc"
)

// Command defines the interface for bot commands
type Command interface {
	Name() string
	Execute(ctx irc.ChatContextInterface)
	AdminOnly() bool
}

// LockRequired is an optional interface for a +command that must queue behind
// the request lock, e.g. one that calls the model.
type LockRequired interface {
	RequiresLock() bool
}

// BypassesLock reports whether a command runs without the request lock. Named +commands do, so
// admins can act during a long render; chat to the model (the default command) never does.
func (r *Registry) BypassesLock(name string) bool {
	if name == "" {
		return false
	}
	cmd, ok := r.commands[name]
	if !ok || cmd == r.defaultCommand {
		return false
	}
	if l, ok := cmd.(LockRequired); ok && l.RequiresLock() {
		return false
	}
	return true
}

// Registry manages command registration and dispatch
type Registry struct {
	commands       map[string]Command
	defaultCommand Command
}

// NewRegistry creates a new command registry
func NewRegistry() *Registry {
	return &Registry{
		commands: make(map[string]Command),
	}
}

// Register adds a command to the registry
// Commands with empty name are registered as the default fallback
func (r *Registry) Register(cmd Command) {
	name := cmd.Name()
	if name == "" {
		r.defaultCommand = cmd
		return
	}
	r.commands[name] = cmd
}

// Get retrieves a command by name
func (r *Registry) Get(name string) (Command, bool) {
	cmd, ok := r.commands[name]
	return cmd, ok
}

// Dispatch executes the appropriate command based on context
// Returns true if a command was executed, false otherwise
func (r *Registry) Dispatch(ctx irc.ChatContextInterface) bool {
	cmdName := ctx.GetCommand()

	cmd, ok := r.commands[cmdName]
	if !ok {
		// Use default command if no match
		if r.defaultCommand != nil {
			r.defaultCommand.Execute(ctx)
			return true
		}
		return false
	}

	// Check admin permission
	if cmd.AdminOnly() && !ctx.IsAdmin() {
		ctx.GetLogger().Info("command_denied", "command", cmdName, "source", ctx.GetSource())
		ctx.Reply("You don't have permission to perform this action.")
		return true
	}

	args := ctx.GetArgs()
	ctx.GetLogger().Info("command_executed",
		"command", cmdName,
		"args", strings.Join(args[min(1, len(args)):], " "),
		"source", ctx.GetSource(),
	)

	cmd.Execute(r.withPrefix(ctx))
	return true
}

// All returns all registered commands (excluding default)
func (r *Registry) All() []Command {
	cmds := make([]Command, 0, len(r.commands))
	for _, cmd := range r.commands {
		cmds = append(cmds, cmd)
	}
	return cmds
}

// prefixedReplies shows command names in replies with the configured prefix,
// since commands spell themselves "+name" in their usage text.
type prefixedReplies struct {
	irc.ChatContextInterface
	names *regexp.Regexp
}

func (r *Registry) withPrefix(ctx irc.ChatContextInterface) irc.ChatContextInterface {
	prefix := ctx.GetConfig().Bot.CommandPrefix
	if prefix == "" || prefix == "+" {
		return ctx
	}
	var alts []string
	for name := range r.commands {
		if strings.HasPrefix(name, "+") {
			alts = append(alts, regexp.QuoteMeta(name[1:]))
		}
	}
	sort.Slice(alts, func(i, j int) bool { return len(alts[i]) > len(alts[j]) })
	return prefixedReplies{ctx, regexp.MustCompile(`(^|[^\w+])\+(` + strings.Join(alts, "|") + `)\b`)}
}

func (p prefixedReplies) rewrite(s string) string {
	prefix := p.GetConfig().Bot.CommandPrefix
	return p.names.ReplaceAllStringFunc(s, func(m string) string {
		i := strings.Index(m, "+")
		return m[:i] + prefix + m[i+1:]
	})
}

func (p prefixedReplies) Reply(s string)       { p.ChatContextInterface.Reply(p.rewrite(s)) }
func (p prefixedReplies) ReplyAction(s string) { p.ChatContextInterface.ReplyAction(p.rewrite(s)) }
