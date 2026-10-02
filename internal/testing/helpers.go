// Copyright (C) 2023-2026 Alex Schlessinger and soulshack contributors
// Modified 2026 by BareMetal
// SPDX-License-Identifier: GPL-3.0-only

package testing

import (
	"time"

	"B4reMetal/metald/internal/config"
)

// DefaultTestConfig returns a minimal configuration for testing
func DefaultTestConfig() *config.Configuration {
	return &config.Configuration{
		Server: &config.ServerConfig{
			Nick:    "testbot",
			Server:  "irc.test.local",
			Port:    6667,
			Channel: "#test",
			SSL:     false,
		},
		Bot: &config.BotConfig{
			Admins:             []string{},
			Verbose:            false,
			Addressed:          true,
			Prompt:             "You are a test bot.",
			Greeting:           "hello",
			Tools:              []string{},
			ShowThinkingAction: false,
			ShowToolActions:    false,
			PromptFloor:        true,
			CommandPrefix:      "+",
			// Non-empty so the mock passes the startup check; content is
			// irrelevant to tests that don't assert on it.
			FloorPrompt:        "test floor prompt\n\n",
			GatekeeperPreamble: "test gatekeeper preamble",
			GatekeeperPolicy:   "test gatekeeper policy",
			ClassifyPreamble:   "test classify preamble",
			ReplyScreenPolicy:  "test reply screen policy",
			MemoryPolicy:       "test memory policy",
			MemoryFrame:        "things you already know about {nick}:",
			RecapPrompt:        "summarise the conversation",
			RecapFrame:         "earlier in this channel:",
			BacklogFrame:       "recent channel lines:",
			RelevantFrame:      "other things you remember:",
			ToolRetryNote:      "call a tool by one of these exact names: {tools}",
			EmptyReplyNote:     "you wrote nothing; answer now",
			TaskPrompt:         "you are working on a background task",
			GoalPrompt:         "goal: {objective}\ndone when: {criteria}\nrounds: {rounds}",
			GoalRoundPrompt:    "round {round} of {rounds}. reviewer: {feedback}\nchecklist:\n{todo}\nnotes:\n{notes}",
			GoalVerifyPrompt:   "answer DONE, CONTINUE: <what's missing> or STUCK: <why>",
			DelegatePrompt:     "answer the one question you are given",
		},
		Model: &config.ModelConfig{
			Model:          "test/model",
			MaxTokens:      100,
			Temperature:    0.7,
			TopP:           1.0,
			ThinkingEffort: "off",
		},
		Session: &config.SessionConfig{
			ChunkMax:      350,
			MaxContext:    100000,
			TTL:           time.Minute * 10,
			Backlog:       30,
			BacklogWindow: 20 * time.Minute,
			RecapMax:      6000,
			MaxIterations: 10,

			TaskMaxTime:       30 * time.Minute,
			TaskMaxIterations: 30,
			TaskMaxTokens:     65536,
			TaskDailyLimit:    3,
			TaskMaxActive:     1,

			GoalMaxRuns:        20,
			GoalMaxTime:        60 * time.Minute,
			GoalDeadline:       24 * time.Hour,
			GoalUpdateInterval: 5 * time.Minute,
		},
		API: &config.APIConfig{
			Timeout: time.Second * 30,
		},
	}
}

// KickCall records a Kick() invocation
type KickCall struct {
	Channel string
	Nick    string
	Reason  string
}

// ModeCall records a Mode() invocation
type ModeCall struct {
	Channel string
	Mode    string
	Target  string
}

// TopicCall records a Topic() invocation
type TopicCall struct {
	Channel string
	Topic   string
}

// OperCall records an Oper() invocation
type OperCall struct {
	Channel string
	Nick    string
}
