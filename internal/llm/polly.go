// Copyright (C) 2023-2026 Alex Schlessinger and soulshack contributors
// Modified 2026 by BareMetal
// SPDX-License-Identifier: GPL-3.0-only

package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/alexschlessinger/pollytool/llm"
	"github.com/alexschlessinger/pollytool/messages"
	"github.com/alexschlessinger/pollytool/tools"

	"B4reMetal/metald/internal/config"
	"B4reMetal/metald/internal/core"
	"B4reMetal/metald/internal/irc"
)

// PollyLLM wraps pollytool's MultiPass and Agent to implement metald's LLM interface
type PollyLLM struct {
	client *llm.MultiPass
}

// NewPollyLLM creates a new pollytool-based LLM client
func NewPollyLLM(config config.APIConfig) *PollyLLM {
	apiKeys := map[string]string{
		"openai":    config.OpenAIKey,
		"anthropic": config.AnthropicKey,
		"gemini":    config.GeminiKey,
		"ollama":    config.OllamaKey,
	}
	return &PollyLLM{client: llm.NewMultiPass(apiKeys)}
}

// ChatCompletionStream returns a channel of string chunks for IRC output
func (p *PollyLLM) ChatCompletionStream(chatCtx core.ChatContextInterface, req *CompletionRequest) <-chan string {
	cfg := chatCtx.GetConfig()
	// Only apply OllamaURL for ollama/ models
	if strings.HasPrefix(req.Model, "ollama/") && cfg.API.OllamaURL != "" {
		req.BaseURL = cfg.API.OllamaURL
	}

	maxChunkSize := 400
	if cfg.Session.ChunkMax > 0 {
		maxChunkSize = cfg.Session.ChunkMax
	}

	output := make(chan string, 10)

	go func() {
		defer close(output)

		// A nil registry is what denies tools under a custom prompt: the model is never told they
		// exist, and a tool call emitted from memory cannot resolve.
		registry := chatCtx.GetSystem().GetToolRegistry()
		if core.Prompts().Active(chatCtx.GetLockKey()) {
			registry = nil
			chatCtx.GetLogger().Debug("tool_registry_withheld_custom_prompt")
		}

		agent := llm.NewAgent(p.client, registry, llm.AgentConfig{
			MaxIterations: 10,
			ToolTimeout:   cfg.API.Timeout,
		})

		chunker := irc.NewChunker(output, maxChunkSize)
		cb := newCallbackHandler(chatCtx, chunker, cfg)

		resp, err := agent.Run(chatCtx, req, cb.build())

		// Flush only if this request is still wanted.
		if err := chatCtx.Err(); errors.Is(err, context.Canceled) {
			// Cancelled (+reset, an ignore): the exchange never happened.
			takePending(req)
			chatCtx.GetLogger().Debug("output_discarded_cancelled")
			return
		}

		// A run that failed does not get to finish its sentence.
		if err != nil {
			chatCtx.GetLogger().Error("agent_error", "error", err.Error())
			held := cb.discard()
			queued := drainQueued(output)
			if held > 0 || queued > 0 {
				chatCtx.GetLogger().Warn("dead_request_output_voided",
					"buffered_bytes", held, "queued_lines", queued)
			}
			// Keep the question so the conversation still shows it was asked.
			commitExchange(chatCtx.GetSession(), takePending(req))
			output <- genericBackendError
			return
		}

		cb.flush()

		commitExchange(chatCtx.GetSession(),
			append(takePending(req), redactRefusedArguments(resp.AllMessages)...))

		source := chatCtx.GetSource()
		if score := core.Suspicions().Score(chatCtx.GetNetwork(), source); score >= core.SuspicionQuarantine {
			if n := core.QuarantineSpeaker(chatCtx.GetSession(), source); n > 0 {
				chatCtx.GetLogger().Warn("exchange_quarantined",
					"source", source, "messages", n,
					"suspicion", score, "cause", "score_threshold")
			}
			core.Suspicions().Discount(chatCtx.GetNetwork(), source, core.SuspicionQuarantine)
		}
	}()

	return output
}

// refusedResult reports whether a tool result is a safety refusal.
func refusedResult(content string) bool {
	c := strings.ToLower(strings.TrimSpace(content))
	return strings.HasPrefix(c, "refused") || strings.HasPrefix(c, "error: refused")
}

// redactRefusedArguments strips the arguments of any tool call that was refused, before the turn is
// written to the session.
func redactRefusedArguments(msgs []messages.ChatMessage) []messages.ChatMessage {
	refused := map[string]bool{}
	for _, m := range msgs {
		if m.ToolCallID != "" && refusedResult(m.GetContent()) {
			refused[m.ToolCallID] = true
		}
	}
	if len(refused) == 0 {
		return msgs
	}

	out := make([]messages.ChatMessage, len(msgs))
	copy(out, msgs)
	for i := range out {
		if len(out[i].ToolCalls) == 0 {
			continue
		}
		calls := make([]messages.ChatMessageToolCall, len(out[i].ToolCalls))
		copy(calls, out[i].ToolCalls)
		for j := range calls {
			if refused[calls[j].ID] {
				calls[j].Arguments = `{"redacted":"refused by a safety check"}`
			}
		}
		out[i].ToolCalls = calls
	}
	return out
}

// callbackHandler organizes callback construction
type callbackHandler struct {
	chatCtx          core.ChatContextInterface
	chunker          *irc.Chunker
	cfg              *config.Configuration
	startTime        time.Time
	lastThinkingTime time.Time
	toolCount        int
	announcedTools   map[string]bool
	lastToolURL      string              // most recent "url: ..." a successful tool call handed back this request
	hadContent       bool                // whether the model's final reply ever wrote any actual content
	leaked           bool                // this turn emitted tool-call syntax; suppress the rest
	leakedBuf        string              // rolling tail, so a marker split across chunks is still seen
	reasoning        irc.ReasoningFilter // strips think blocks the model wrote into content
	reasoningBlocks  int                 // last logged value of reasoning.Blocks
	reasoningStray   int                 // last logged value of reasoning.Stray
	contentBytes     int                 // visible content this turn has streamed
	overBudget       bool                // budget spent; suppress the rest of the turn
}

// maxTurnContent bounds how much visible text ONE request may post.
const maxTurnContent = 2000

func newCallbackHandler(chatCtx core.ChatContextInterface, chunker *irc.Chunker, cfg *config.Configuration) *callbackHandler {
	return &callbackHandler{
		chatCtx:        chatCtx,
		chunker:        chunker,
		cfg:            cfg,
		startTime:      time.Now(),
		announcedTools: make(map[string]bool),
	}
}

func (h *callbackHandler) build() *llm.AgentCallbacks {
	return &llm.AgentCallbacks{
		OnReasoning:       h.onReasoning,
		OnContent:         h.onContent,
		BeforeToolExecute: h.beforeToolExecute,
		OnToolStart:       h.onToolStart,
		OnToolEnd:         h.onToolEnd,
		OnComplete:        h.onComplete,
		OnError:           h.onError,
	}
}

func (h *callbackHandler) onComplete(response *messages.ChatMessage) {
	duration := time.Since(h.startTime)
	inputTokens := response.GetInputTokens()
	outputTokens := response.GetOutputTokens()

	fields := []any{"duration_ms", duration.Milliseconds()}
	if inputTokens > 0 || outputTokens > 0 {
		fields = append(fields, "input_tokens", inputTokens, "output_tokens", outputTokens)
	}
	if h.toolCount > 0 {
		fields = append(fields, "tool_count", h.toolCount)
	}
	h.chatCtx.GetLogger().Info("request_complete", fields...)

	if (!h.hadContent || h.overBudget) && h.lastToolURL != "" {
		h.chatCtx.GetLogger().Warn("empty_final_reply_fallback", "url", h.lastToolURL)
		h.overBudget = false // let the url itself through
		h.chunker.Write(h.lastToolURL + "\n")
	}
}

func (h *callbackHandler) onReasoning(content string) {
	h.chatCtx.GetLogger().Debug("reasoning_chunk", "content", content)

	if !h.cfg.Bot.ShowThinkingAction {
		return
	}

	now := time.Now()
	if now.Sub(h.startTime) > 30*time.Second {
		if h.lastThinkingTime.IsZero() || now.Sub(h.lastThinkingTime) > 30*time.Second {
			elapsed := now.Sub(h.startTime).Round(time.Second)
			h.chatCtx.ReplyAction(fmt.Sprintf("is thinking... (%s)", elapsed))
			h.lastThinkingTime = now
		}
	}
}

func (h *callbackHandler) onContent(content string) {
	h.chatCtx.GetLogger().Debug("oncontent_callback",
		"content", content,
		"content_len", len(content),
	)

	h.leakedBuf += content
	if !h.leaked && irc.LooksLikeToolCall(h.leakedBuf) {
		h.leaked = true
		score := core.Suspicions().Add(h.chatCtx.GetNetwork(), h.chatCtx.GetSource(), core.SignalToolSyntax)
		h.chatCtx.GetLogger().Warn("tool_syntax_latched",
			"preview", truncateForLog(h.leakedBuf), "suspicion", score)
	}
	if h.leaked {
		return
	}
	// Only the tail can begin a split marker, so the buffer stays bounded.
	if len(h.leakedBuf) > 256 {
		h.leakedBuf = h.leakedBuf[len(h.leakedBuf)-256:]
	}

	content = h.reasoning.Feed(content)
	if h.reasoning.Blocks != h.reasoningBlocks || h.reasoning.Stray != h.reasoningStray {
		h.reasoningBlocks, h.reasoningStray = h.reasoning.Blocks, h.reasoning.Stray
		h.chatCtx.GetLogger().Warn("reasoning_in_content_stripped",
			"blocks", h.reasoning.Blocks, "stray_closers", h.reasoning.Stray)
	}

	// Budget latch.
	if h.overBudget {
		return
	}
	h.contentBytes += len(content)
	if h.contentBytes > maxTurnContent {
		h.overBudget = true
		score := core.Suspicions().Add(h.chatCtx.GetNetwork(), h.chatCtx.GetSource(), core.SignalRunaway)
		h.chatCtx.GetLogger().Warn("turn_content_budget_exceeded",
			"bytes", h.contentBytes, "limit", maxTurnContent,
			"preview", truncateForLog(content), "suspicion", score)
		return
	}

	if content != "" {
		h.hadContent = true
	}
	h.chunker.Write(content)
}

// discard drops everything a dead request was still holding and returns how much it dropped.
func (h *callbackHandler) discard() int {
	h.reasoning.Flush()
	return h.chunker.Discard()
}

// flush releases anything the reasoning filter was holding back, then flushes the chunker.
func (h *callbackHandler) flush() {
	if tail := h.reasoning.Flush(); tail != "" {
		h.hadContent = true
		h.chunker.Write(tail)
	}
	h.chunker.Flush()
}

func truncateForLog(s string) string {
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}

func (h *callbackHandler) beforeToolExecute(ctx context.Context, tc messages.ChatMessageToolCall, args map[string]any) context.Context {
	return irc.InjectContext(ctx, h.chatCtx)
}

// toolDisplayName is what the channel sees in "calling X".
var toolDisplayOverrides = map[string]string{
	"musicgen__song":    "musicgen",
	"imagegen__picture": "imagegen",
}

func toolDisplayName(name string) string {
	if d, ok := toolDisplayOverrides[name]; ok {
		return d
	}
	if idx := strings.Index(name, "__"); idx != -1 {
		return name[idx+2:]
	}
	return name
}

func (h *callbackHandler) onToolStart(calls []messages.ChatMessageToolCall) {
	h.flush()

	h.toolCount += len(calls)

	// Log each tool
	for _, tc := range calls {
		h.chatCtx.GetLogger().Info("tool_started", "tool", tc.Name)
	}

	if !h.cfg.Bot.ShowToolActions || len(calls) == 0 {
		return
	}

	// Filter and format tool names, skipping ones already announced this request - a retried tool call
	// (e.g. after a failed safety check) shouldn't re-announce "calling X" a second time.
	var names []string
	for _, tc := range calls {
		if tc.Name == "irc__action" {
			continue
		}
		displayName := toolDisplayName(tc.Name)
		if h.announcedTools[displayName] {
			continue
		}
		h.announcedTools[displayName] = true
		names = append(names, displayName)
	}

	if len(names) > 0 {
		h.chatCtx.ReplyAction(fmt.Sprintf("calling %s", strings.Join(names, ", ")))
	}
}

func (h *callbackHandler) onToolEnd(tc messages.ChatMessageToolCall, result string, duration time.Duration, toolErr error) {
	if toolErr != nil {
		h.chatCtx.GetLogger().Error("tool_failed",
			"tool", tc.Name,
			"duration_ms", duration.Milliseconds(),
			"error", toolErr.Error(),
		)
		return
	}

	// A tool refusing what it was handed is the strongest single signal available.
	if isToolRefusal(result) {
		score := core.Suspicions().Add(h.chatCtx.GetNetwork(), h.chatCtx.GetSource(), core.SignalToolRefused)
		h.chatCtx.GetLogger().Warn("tool_refused_request",
			"tool", tc.Name, "source", h.chatCtx.GetSource(), "suspicion", score)
	}

	preview := result
	if len(preview) > 60 && !h.cfg.Bot.Verbose {
		preview = preview[:60] + "..."
	}
	h.chatCtx.GetLogger().Info("tool_completed",
		"tool", tc.Name,
		"duration_ms", duration.Milliseconds(),
		"result_size", len(result),
		"preview", preview,
	)

	// Remember the last url a tool returned, as a fallback if the model's final reply is empty.
	if firstLine, ok := strings.CutPrefix(result, "url: "); ok {
		if nl := strings.IndexByte(firstLine, '\n'); nl != -1 {
			firstLine = firstLine[:nl]
		}
		h.lastToolURL = strings.TrimSpace(firstLine)
	}
}

// drainQueued empties a buffered channel without blocking, returning how many items it threw away.
func drainQueued(ch chan string) int {
	n := 0
	for {
		select {
		case <-ch:
			n++
		default:
			return n
		}
	}
}

// isToolRefusal reports whether a tool result is a refusal rather than an ordinary failure.
func isToolRefusal(result string) bool {
	return strings.HasPrefix(strings.TrimSpace(strings.ToLower(result)), "error: refused")
}

// genericBackendError is what the channel sees when the LLM backend fails.
const genericBackendError = "something went wrong talking to my backend. try again in a moment."

func (h *callbackHandler) onError(err error) {
	h.chatCtx.GetLogger().Error("stream_error", "error", err.Error())
}

// CreateAgentForRegistry creates an agent with the given registry for external use
func CreateAgentForRegistry(client *llm.MultiPass, registry *tools.ToolRegistry, timeout time.Duration) *llm.Agent {
	return llm.NewAgent(client, registry, llm.AgentConfig{
		MaxIterations: 10,
		ToolTimeout:   timeout,
	})
}
