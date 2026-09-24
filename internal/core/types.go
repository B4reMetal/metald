// Copyright (C) 2023-2026 Alex Schlessinger and soulshack contributors
// SPDX-License-Identifier: GPL-3.0-only

package core

// ChannelInfo represents the state of an IRC-ish channel
type ChannelInfo struct {
	Name  string
	Modes string
	Topic string
}

// UserInfo represents detailed information about an IRC-ish user
type UserInfo struct {
	Nick     string
	Ident    string
	Host     string
	RealName string
	Account  string
	Away     string
	Channels []string
}

// ChannelUser represents a user in the context of a channel
type ChannelUser struct {
	Nick    string
	IsOp    bool
	IsVoice bool
}
