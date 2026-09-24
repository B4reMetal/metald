// Copyright (C) 2023-2026 Alex Schlessinger and soulshack contributors
// Modified 2026 by BareMetal
// SPDX-License-Identifier: GPL-3.0-only

package commands

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"B4reMetal/metald/internal/irc"

	"github.com/alexschlessinger/pollytool/tools"
)

// ToolsCommand handles the +tools command for managing tools
type ToolsCommand struct{}

func (c *ToolsCommand) Name() string    { return "+tools" }
func (c *ToolsCommand) AdminOnly() bool { return false } // We handle permissions internally

func (c *ToolsCommand) Execute(ctx irc.ChatContextInterface) {
	args := ctx.GetArgs()

	// If no arguments, list tools (equivalent to old /get tools)
	if len(args) < 2 {
		ctx.Reply("Usage: +tools [list|load|rm|restrict|unrestrict] <args>")
		return
	}

	subcommand := args[1]
	rest := ""
	if len(args) > 2 {
		rest = strings.Join(args[2:], " ")
	}

	// Handle non-admin subcommands first
	if subcommand == "list" {
		c.listNamespace(ctx, rest)
		return
	}

	// Other subcommands require admin privileges
	if !ctx.IsAdmin() {
		ctx.Reply("You don't have permission to perform this action.")
		return
	}

	switch subcommand {
	case "load":
		fallthrough
	case "add":
		c.addTool(ctx, rest)
	case "rm":
		fallthrough
	case "remove":
		c.removeTool(ctx, rest)
	case "restrict":
		c.setRestriction(ctx, rest, true)
	case "unrestrict":
		fallthrough
	case "allow":
		c.setRestriction(ctx, rest, false)
	default:
		ctx.Reply("Usage: +tools [list|load|rm|restrict|unrestrict] <args>")
	}
}

// setRestriction moves tools in or out of admin-only at runtime, without a restart.
func (c *ToolsCommand) setRestriction(ctx irc.ChatContextInterface, pattern string, restrict bool) {
	verb := "unrestrict"
	if restrict {
		verb = "restrict"
	}
	if pattern == "" {
		ctx.Reply(fmt.Sprintf("Usage: +tools %s <tool|namespace|pattern>", verb))
		return
	}

	registry := ctx.GetSystem().GetToolRegistry()
	matches := matchToolNames(registry.All(), pattern)
	if len(matches) == 0 {
		ctx.Reply(fmt.Sprintf("No tools matched: %s", pattern))
		return
	}

	var changed []string
	for _, name := range matches {
		tool, ok := registry.Get(name)
		if !ok {
			continue
		}
		if irc.IsAdminOnly(tool) == restrict {
			continue // already in the requested state
		}
		if restrict {
			registry.Register(irc.NewAdminOnlyTool(tool))
		} else {
			registry.Register(irc.UnwrapAdminOnly(tool))
		}
		changed = append(changed, name)
	}

	if len(changed) == 0 {
		state := "unrestricted"
		if restrict {
			state = "already admin-only"
		}
		ctx.Reply(fmt.Sprintf("No change - matched tools are %s", state))
		return
	}

	syncAdminToolsConfig(ctx, changed, restrict)
	PersistAdminTools(ctx.GetConfig().Bot.AdminTools)
	ctx.GetLogger().Info("tool_restriction_changed",
		"tools", strings.Join(changed, ","), "admin_only", restrict, "by", ctx.GetSource())

	if restrict {
		ctx.Reply(fmt.Sprintf("Restricted to admins: %s", strings.Join(changed, ", ")))
	} else {
		ctx.Reply(fmt.Sprintf("Available to everyone: %s", strings.Join(changed, ", ")))
	}
}

// syncAdminToolsConfig keeps cfg.Bot.AdminTools consistent with the live registry, so "+get
// admintools" reflects reality.
func syncAdminToolsConfig(ctx irc.ChatContextInterface, names []string, restrict bool) {
	cfg := ctx.GetConfig()
	current := make(map[string]bool, len(cfg.Bot.AdminTools))
	for _, n := range cfg.Bot.AdminTools {
		current[n] = true
	}
	for _, n := range names {
		if restrict {
			current[n] = true
		} else {
			delete(current, n)
		}
	}
	updated := make([]string, 0, len(current))
	for n := range current {
		updated = append(updated, n)
	}
	sort.Strings(updated)
	cfg.Bot.AdminTools = updated
}

// matchToolNames resolves an exact name, a bare namespace, or a wildcard
// pattern against the loaded tools.
func matchToolNames(all []tools.Tool, pattern string) []string {
	// A bare word with no wildcard and no "__" means "the whole namespace".
	if !strings.Contains(pattern, "*") && !strings.Contains(pattern, "__") {
		pattern += "__*"
	}

	var matched []string
	for _, tool := range all {
		name := tool.GetName()
		if name == pattern {
			return []string{name}
		}
		if ok, _ := path.Match(pattern, name); ok {
			matched = append(matched, name)
		}
	}
	sort.Strings(matched)
	return matched
}

func (c *ToolsCommand) listTools(ctx irc.ChatContextInterface) {
	registry := ctx.GetSystem().GetToolRegistry()
	allTools := registry.All()

	if len(allTools) == 0 {
		ctx.Reply("No tools loaded")
		return
	}

	var toolNames []string
	for _, tool := range allTools {
		toolNames = append(toolNames, tool.GetName())
	}

	message := formatToolList(toolNames)
	ctx.Reply(truncateMessage(message, ctx.GetConfig().Session.ChunkMax))
}

func (c *ToolsCommand) listNamespace(ctx irc.ChatContextInterface, namespace string) {
	if namespace == "" {
		c.listTools(ctx)
		return
	}

	registry := ctx.GetSystem().GetToolRegistry()
	allTools := registry.All()

	prefix := namespace + "__"

	var toolNames []string
	for _, tool := range allTools {
		name := tool.GetName()
		if strings.HasPrefix(name, prefix) {
			_, bareName := parseToolName(name)
			entry := namespace + "__" + bareName
			// Mark restrictions so the listing shows who can actually use
			// each tool, which now changes at runtime via +tools restrict.
			if irc.IsAdminOnly(tool) {
				entry += " [admin]"
			}
			toolNames = append(toolNames, entry)
		}
	}

	if len(toolNames) == 0 {
		ctx.Reply(fmt.Sprintf("No tools in namespace: %s", namespace))
		return
	}

	message := strings.Join(toolNames, ", ")
	ctx.Reply(truncateMessage(message, ctx.GetConfig().Session.ChunkMax))
}

// parseToolName extracts namespace and bare name from a tool name
// e.g., "git__status" -> ("git", "status")
// e.g., "irc__op" -> ("irc", "op")
func parseToolName(name string) (namespace, bareName string) {
	if idx := strings.Index(name, "__"); idx != -1 {
		return name[:idx], name[idx+2:]
	}
	return "other", name
}

func (c *ToolsCommand) addTool(ctx irc.ChatContextInterface, toolPath string) {
	if toolPath == "" {
		ctx.Reply("Usage: +tools add <path>")
		return
	}

	registry := ctx.GetSystem().GetToolRegistry()
	result, err := registry.LoadToolAuto(toolPath)
	if err != nil {
		ctx.Reply(fmt.Sprintf("Failed: %v", err))
		return
	}

	ctx.Reply(formatLoadResult(result))
}

// formatLoadResult creates a compact message for all loaded tools
func formatLoadResult(result tools.LoadResult) string {
	if len(result.Servers) == 0 {
		return "No tools loaded"
	}

	groups := make(map[string]int)
	var order []string
	for _, server := range result.Servers {
		groups[server.Name] = len(server.ToolNames)
		order = append(order, server.Name)
	}
	return fmt.Sprintf("Added: %s", formatGroupedSummary(groups, order))
}

func (c *ToolsCommand) removeTool(ctx irc.ChatContextInterface, pattern string) {
	if pattern == "" {
		ctx.Reply("Usage: +tools remove <name or pattern>")
		return
	}

	registry := ctx.GetSystem().GetToolRegistry()

	// Check if this is a namespace removal (plain name or name__*)
	isNamespaceRemoval := false
	namespace := pattern
	if !strings.Contains(pattern, "*") && !strings.Contains(pattern, "__") {
		isNamespaceRemoval = true
	} else if strings.HasSuffix(pattern, "__*") {
		isNamespaceRemoval = true
		namespace = strings.TrimSuffix(pattern, "__*")
	}

	// If no wildcards and no __, treat as namespace prefix
	if !strings.Contains(pattern, "*") && !strings.Contains(pattern, "__") {
		pattern = pattern + "__*"
	}

	// Use wildcard matching
	if strings.Contains(pattern, "*") {
		var removed []string
		for _, tool := range registry.All() {
			name := tool.GetName()
			matched, _ := path.Match(pattern, name)
			if matched {
				registry.Remove(name)
				removed = append(removed, name)
			}
		}

		if len(removed) > 0 {
			if isNamespaceRemoval {
				ctx.Reply(fmt.Sprintf("Removed: %s", namespace))
			} else {
				ctx.Reply(fmt.Sprintf("Removed: %s", formatToolList(removed)))
			}
		} else {
			ctx.Reply(fmt.Sprintf("No tools matched: %s", pattern))
		}
	} else {
		// Exact match
		if _, exists := registry.Get(pattern); !exists {
			ctx.Reply(fmt.Sprintf("Not found: %s", pattern))
		} else {
			registry.Remove(pattern)
			_, bareName := parseToolName(pattern)
			ctx.Reply(fmt.Sprintf("Removed: %s", bareName))
		}
	}
}

// formatToolList formats a list of tool names in grouped format
func formatToolList(toolNames []string) string {
	groups := make(map[string]int)
	var order []string

	for _, name := range toolNames {
		namespace, _ := parseToolName(name)
		if _, exists := groups[namespace]; !exists {
			order = append(order, namespace)
		}
		groups[namespace]++
	}

	return formatGroupedSummary(groups, order)
}

// formatGroupedSummary formats namespace counts as "ns1 (N tools), ns2 (M tools)"
func formatGroupedSummary(groups map[string]int, order []string) string {
	var parts []string
	for _, ns := range order {
		count := groups[ns]
		if count == 1 {
			parts = append(parts, fmt.Sprintf("%s (1 tool)", ns))
		} else {
			parts = append(parts, fmt.Sprintf("%s (%d tools)", ns, count))
		}
	}
	return strings.Join(parts, ", ")
}

// truncateMessage truncates a message to maxLen with ellipsis
func truncateMessage(message string, maxLen int) string {
	if maxLen <= 0 {
		maxLen = 350
	}
	if len(message) > maxLen {
		return message[:maxLen-3] + "..."
	}
	return message
}
