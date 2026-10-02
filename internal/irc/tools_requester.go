// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package irc

import (
	"context"
	"maps"

	"github.com/alexschlessinger/pollytool/tools"
)

// RequesterTool passes the nick that asked for a tool call to the tool as "requested_by", for tools
// that credit or record who asked. The value comes from the IRC context, never from the model: any
// requested_by the model wrote is replaced.
type RequesterTool struct {
	tools.Tool
}

// NewRequesterTool wraps tool so each call carries the asking nick.
func NewRequesterTool(tool tools.Tool) *RequesterTool {
	return &RequesterTool{Tool: tool}
}

func (r *RequesterTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	args = maps.Clone(args)
	if args == nil {
		args = map[string]any{}
	}
	delete(args, "requested_by")
	if chatCtx, err := GetIRCContext(ctx); err == nil {
		args["requested_by"] = chatCtx.GetSource()
	}
	return r.Tool.Execute(ctx, args)
}
