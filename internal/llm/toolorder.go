// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package llm

import (
	"context"
	"sort"

	"github.com/alexschlessinger/pollytool/llm"
	"github.com/alexschlessinger/pollytool/messages"
)

// stableToolOrder sorts a request's tools by name before it goes out. pollytool rebuilds the list
// from a map before every model call, so it arrives in a different order each time; the chat
// template writes tools at the very top of the prompt, and a reshuffled top means the backend can
// never reuse its cache of the conversation.
type stableToolOrder struct{ llm.LLM }

func (s stableToolOrder) ChatCompletionStream(ctx context.Context, req *llm.CompletionRequest, p llm.EventStreamProcessor) <-chan *messages.StreamEvent {
	sort.SliceStable(req.Tools, func(i, j int) bool { return req.Tools[i].GetName() < req.Tools[j].GetName() })
	return s.LLM.ChatCompletionStream(ctx, req, p)
}
