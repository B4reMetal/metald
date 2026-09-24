// Copyright (C) 2023-2026 Alex Schlessinger and soulshack contributors
// Modified 2026 by BareMetal
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"context"
	"log/slog"

	"github.com/alexschlessinger/pollytool/llm"
	"github.com/alexschlessinger/pollytool/sessions"
	"github.com/alexschlessinger/pollytool/tools"

	"B4reMetal/metald/internal/config"
)

// ChatContextInterface provides all context needed for handling IRC messages
type ChatContextInterface interface {
	context.Context

	// Event methods
	IsAddressed() bool
	IsAdmin() bool
	IsPrivate() bool
	GetCommand() string
	GetSource() string
	GetRequestID() string
	GetArgs() []string

	// Responder methods
	Reply(string)
	ReplyAction(string)
	SendAction(target, message string)

	// Controller methods
	Join(string) bool
	JoinWithKey(channel, key string) bool
	Nick(string) bool
	FatalError(err error)
	SetMode(target, flags string, args ...string) bool
	Kick(channel, nick, reason string) bool
	Ban(channel, target string) bool
	Unban(channel, target string) bool
	Invite(channel, nick string) bool
	Topic(channel, topic string) bool
	Oper(string, string) bool

	// State methods
	GetUser(nick string) *UserInfo
	GetChannel(name string) *ChannelInfo
	GetChannelUsers(channel string) []ChannelUser
	GetBotNick() string
	GetLockKey() string
	// GetNetwork names the IRC network this request arrived on. Empty on a
	// single-network bot. Scopes per-network data such as memories.
	GetNetwork() string
	IsOp(channel, nick string) bool

	// Runtime methods
	GetSession() sessions.Session
	GetConfig() *config.Configuration
	GetSystem() System
	GetLogger() *slog.Logger
}

// LLM defines the interface for the language model client
type LLM interface {
	// ChatCompletionStream returns a channel of string chunks for IRC output
	ChatCompletionStream(ChatContextInterface, *llm.CompletionRequest) <-chan string
}

type System interface {
	GetToolRegistry() *tools.ToolRegistry
	GetSessionStore() sessions.SessionStore
	GetLLM() LLM
	UpdateLLM(config.APIConfig) error
}
