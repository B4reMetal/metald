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
	client llm.LLM
}

// NewPollyLLM creates a new pollytool-based LLM client
func NewPollyLLM(config config.APIConfig) *PollyLLM {
	apiKeys := map[string]string{
		"openai":    config.OpenAIKey,
		"anthropic": config.AnthropicKey,
		"gemini":    config.GeminiKey,
		"ollama":    config.OllamaKey,
	}
	return &PollyLLM{client: stableToolOrder{llm.NewMultiPass(apiKeys)}}
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
		registry := toolView(chatCtx.GetSystem().GetToolRegistry(), scopeOf(chatCtx.GetSession().GetName()))
		if core.Prompts().Active(chatCtx.GetLockKey()) {
			registry = nil
			chatCtx.GetLogger().Debug("tool_registry_withheld_custom_prompt")
		}

		iterations := cfg.Session.MaxIterations
		if iterations <= 0 {
			iterations = 10
		}
		agent := llm.NewAgent(p.client, registry, llm.AgentConfig{
			MaxIterations: iterations,
			ToolTimeout:   cfg.API.Timeout,
		})

		chunker := irc.NewChunker(output, maxChunkSize)
		cb := newCallbackHandler(chatCtx, chunker, cfg)

		resp, err := agent.Run(chatCtx, req, cb.build())

		// A tool call written as text never ran: ask once more, telling the model the exact tool
		// names. Only when no tool has run yet, so a retry cannot repeat one. The note is not
		// committed; only the retry's answer is.
		retried := false
		if err == nil && cb.leaked && cb.toolCount == 0 && registry != nil && chatCtx.Err() == nil {
			retried = true
			chatCtx.GetLogger().Warn("tool_syntax_retry")
			retry := *req
			retry.Messages = append(append([]messages.ChatMessage(nil), req.Messages...), messages.ChatMessage{
				Role:    messages.MessageRoleUser,
				Content: toolRetryNote(cfg.Bot.ToolRetryNote, registry),
			})
			cb = newCallbackHandler(chatCtx, chunker, cfg)
			resp, err = agent.Run(chatCtx, &retry, cb.build())
		}

		// A turn that ended with nothing to show - no text, no tool, no link - usually thought itself
		// out and never wrote the answer. Ask once for the answer itself.
		if err == nil && cb.silent() && !retried && chatCtx.Err() == nil {
			retried = true
			chatCtx.GetLogger().Warn("empty_reply_retry")
			retry := *req
			retry.Messages = append(append([]messages.ChatMessage(nil), req.Messages...), messages.ChatMessage{
				Role:    messages.MessageRoleUser,
				Content: strings.TrimSpace(cfg.Bot.EmptyReplyNote),
			})
			cb = newCallbackHandler(chatCtx, chunker, cfg)
			resp, err = agent.Run(chatCtx, &retry, cb.build())
		}

		// Flush only if this request is still wanted.
		if err := chatCtx.Err(); errors.Is(err, context.Canceled) {
			// Cancelled (+reset, an ignore): the exchange never happened.
			takePending(req)
			chatCtx.GetLogger().Debug("output_discarded_cancelled")
			return
		}

		// Out of iterations is not a backend failure: keep what was said and the work done so far.
		if err != nil && err.Error() == "max iterations exceeded" && resp != nil {
			chatCtx.GetLogger().Warn("agent_max_iterations", "iterations", resp.IterationCount)
			cb.flush()
			commitExchange(chatCtx.GetSession(),
				append(takePending(req), redactRefusedArguments(resp.AllMessages)...))
			logReplies(chatCtx, resp.AllMessages)
			if !cb.hadContent {
				output <- outOfSteps
			}
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
		logReplies(chatCtx, resp.AllMessages)
		MaybeFold(cfg, chatCtx.GetSession())

		// Still nothing after the retry: say so rather than leave the channel waiting.
		if cb.silent() {
			chatCtx.GetLogger().Warn("empty_reply_posted_notice")
			output <- noAnswer
		}

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
	refusals         map[string]int      // refusals per tool this request, to stop a refusal loop
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
		refusals:       make(map[string]int),
	}
}

func (h *callbackHandler) build() *llm.AgentCallbacks {
	return &llm.AgentCallbacks{
		OnReasoning:       h.onReasoning,
		OnContent:         h.onContent,
		BeforeToolExecute: h.beforeToolExecute,
		OnToolStart:       h.onToolStart,
		ApproveToolCalls:  h.approveToolCalls,
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
	core.LiveThinking.Publish(core.LiveEvent{Request: h.chatCtx.GetRequestID(), Network: h.chatCtx.GetNetwork(),
		Source: h.chatCtx.GetSource(), Target: h.chatCtx.GetTarget(), Text: content})

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
	h.correctToolNames(calls)

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
		if tc.Name == "irc__action" || core.QuietTool(tc.Name) || !h.toolExists(tc.Name) {
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

	// A plugin posts a /me by starting its result with "action: ".
	if action, ok := toolAction(result); ok {
		h.flush()
		h.chatCtx.ReplyAction(action)
		recordAction(h.chatCtx.GetRequestID(), action)
	}

	if isToolRefusal(result) {
		h.refusals[tc.Name]++
	}

	// The reply may link to what this tool returned.
	learnOwnHosts(result)
	vouchLinks(h.chatCtx.GetRequestID(), result)

	// Remember the last url a tool returned, as a fallback if the model's final reply is empty.
	if firstLine, ok := strings.CutPrefix(result, "url: "); ok {
		if nl := strings.IndexByte(firstLine, '\n'); nl != -1 {
			firstLine = firstLine[:nl]
		}
		h.lastToolURL = strings.TrimSpace(firstLine)
	}
}

// maxToolRefusals is how often one tool may refuse a request before the model stops being allowed
// to call it; past that it only burns iterations asking again.
const maxToolRefusals = 2

// approveToolCalls denies calls to a tool that has already refused this request twice.
func (h *callbackHandler) approveToolCalls(calls []messages.ChatMessageToolCall) []bool {
	ok := make([]bool, len(calls))
	for i, tc := range calls {
		ok[i] = h.refusals[tc.Name] < maxToolRefusals
		if !ok[i] {
			h.chatCtx.GetLogger().Warn("tool_call_denied_after_refusals", "tool", tc.Name, "refusals", h.refusals[tc.Name])
		}
	}
	return ok
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

// noAnswer is what the channel sees when the model twice finished without writing anything.
const noAnswer = "i went quiet on that one - thought it through and never wrote an answer. ask again, maybe more specifically."

// silent reports whether this turn produced nothing the channel saw: no text, no tool, no fallback
// link.
func (h *callbackHandler) silent() bool {
	return !h.hadContent && h.toolCount == 0 && h.lastToolURL == ""
}

// outOfSteps is what the channel sees when a request used every tool iteration without answering.
const outOfSteps = "that took more steps than i'm allowed and i didn't get to an answer. try asking for less at once."

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

const maxToolAction = 300

// toolAction extracts the /me text from a tool result whose first line starts
// with "action: ", stripped of control characters and capped in length.
func toolAction(result string) (string, bool) {
	first, _, _ := strings.Cut(result, "\n")
	text, ok := strings.CutPrefix(first, "action: ")
	if !ok {
		return "", false
	}
	text = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, text)
	text = strings.TrimSpace(text)
	if len(text) > maxToolAction {
		text = strings.TrimSpace(text[:maxToolAction])
	}
	return text, text != ""
}

// toolExists reports whether a call names a registered tool; a misspelled call
// fails anyway and must not be announced.
func (h *callbackHandler) toolExists(name string) bool {
	sys := h.chatCtx.GetSystem()
	if sys == nil || sys.GetToolRegistry() == nil {
		return true
	}
	_, ok := sys.GetToolRegistry().Get(name)
	return ok
}
