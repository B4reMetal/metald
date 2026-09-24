// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package commands

import (
	"fmt"
	"strconv"
	"strings"

	"B4reMetal/metald/internal/core"
	"B4reMetal/metald/internal/irc"
)

// MemoriesCommand inspects and clears what the bot has remembered.
type MemoriesCommand struct{}

func (c *MemoriesCommand) Name() string    { return "+memories" }
func (c *MemoriesCommand) AdminOnly() bool { return false }

func (c *MemoriesCommand) Execute(ctx irc.ChatContextInterface) {
	store, err := core.Memories()
	if err != nil {
		ctx.GetLogger().Error("memory_store_unavailable", "error", err.Error())
		ctx.Reply("memory is unavailable")
		return
	}

	args := ctx.GetArgs()
	sub := ""
	if len(args) > 1 {
		sub = strings.ToLower(args[1])
	}

	switch sub {
	case "", "list":
		c.list(ctx, store, args)

	case "about":
		if len(args) < 3 {
			ctx.Reply("usage: +memories about <nick>")
			return
		}
		c.about(ctx, store, args[2])

	case "forget":
		c.forget(ctx, store, args)

	case "clear":
		c.clear(ctx, store, args)

	default:
		ctx.Reply("usage: +memories [list] | about <nick> | forget <id> | clear [nick]")
	}
}

func (c *MemoriesCommand) list(ctx irc.ChatContextInterface, store *core.MemoryStore, args []string) {
	limit := 10
	if len(args) > 2 {
		if n, err := strconv.Atoi(args[2]); err == nil && n > 0 {
			limit = n
		}
	}

	total, _ := store.Count(ctx.GetNetwork())
	mems, err := store.List(ctx.GetNetwork(), limit)
	if err != nil {
		ctx.GetLogger().Error("memory_list_failed", "error", err.Error())
		ctx.Reply("could not read memory")
		return
	}
	if len(mems) == 0 {
		ctx.Reply("no memories stored")
		return
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%d memory(ies) stored, showing %d most recent:", total, len(mems))
	for _, m := range mems {
		fmt.Fprintf(&b, "\n  [%d] %s: %s", m.ID, m.Subject, m.Fact)
	}
	ctx.Reply(b.String())
}

func (c *MemoriesCommand) about(ctx irc.ChatContextInterface, store *core.MemoryStore, subject string) {
	mems, err := store.Recall(ctx.GetNetwork(), subject, 25)
	if err != nil {
		ctx.GetLogger().Error("memory_recall_failed", "error", err.Error())
		ctx.Reply("could not read memory")
		return
	}
	if len(mems) == 0 {
		ctx.Reply(fmt.Sprintf("nothing remembered about %s", subject))
		return
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%d memory(ies) about %s:", len(mems), subject)
	for _, m := range mems {
		fmt.Fprintf(&b, "\n  [%d] %s", m.ID, m.Fact)
	}
	ctx.Reply(b.String())
}

func (c *MemoriesCommand) forget(ctx irc.ChatContextInterface, store *core.MemoryStore, args []string) {
	if len(args) < 3 {
		ctx.Reply("usage: +memories forget <id>")
		return
	}
	id, err := strconv.ParseInt(args[2], 10, 64)
	if err != nil || id <= 0 {
		ctx.Reply(fmt.Sprintf("%q is not a memory id", args[2]))
		return
	}

	// Same rule the tool enforces: your own memories are yours to delete,
	// everyone else's need an operator.
	mem, found, err := store.Get(ctx.GetNetwork(), id)
	if err != nil {
		ctx.GetLogger().Error("memory_lookup_failed", "error", err.Error())
		ctx.Reply("could not read memory")
		return
	}
	if !found {
		ctx.Reply(fmt.Sprintf("no memory with id %d", id))
		return
	}
	if !strings.EqualFold(mem.Subject, ctx.GetSource()) && !ctx.IsAdmin() {
		ctx.Reply(fmt.Sprintf("memory %d is about %s - only they or an operator can remove it", id, mem.Subject))
		return
	}

	if ok, err := store.Forget(ctx.GetNetwork(), id); err != nil || !ok {
		ctx.Reply("could not forget that")
		return
	}
	ctx.GetLogger().Info("memory_forgotten", "id", id, "subject", mem.Subject, "by", ctx.GetSource())
	ctx.Reply(fmt.Sprintf("forgot memory %d about %s", id, mem.Subject))
}

func (c *MemoriesCommand) clear(ctx irc.ChatContextInterface, store *core.MemoryStore, args []string) {
	// Clearing a single subject is allowed for that person; wiping EVERYTHING is operators only.
	if len(args) > 2 {
		subject := args[2]
		if !strings.EqualFold(subject, ctx.GetSource()) && !ctx.IsAdmin() {
			ctx.Reply(fmt.Sprintf("only %s or an operator can clear memories about them", subject))
			return
		}
		n, err := store.ForgetSubject(ctx.GetNetwork(), subject)
		if err != nil {
			ctx.Reply("could not clear those")
			return
		}
		ctx.GetLogger().Info("memories_cleared", "subject", subject, "count", n, "by", ctx.GetSource())
		ctx.Reply(fmt.Sprintf("cleared %d memory(ies) about %s", n, subject))
		return
	}

	if !ctx.IsAdmin() {
		ctx.Reply("only an operator can wipe every memory - use \"+memories clear <nick>\" for your own")
		return
	}
	n, err := store.Clear(ctx.GetNetwork())
	if err != nil {
		ctx.GetLogger().Error("memory_clear_failed", "error", err.Error())
		ctx.Reply("could not clear memories")
		return
	}
	ctx.GetLogger().Warn("memories_cleared_all", "count", n, "by", ctx.GetSource())
	ctx.Reply(fmt.Sprintf("wiped %d memory(ies)", n))
}
