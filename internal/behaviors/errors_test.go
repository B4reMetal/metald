// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package behaviors

import (
	"testing"

	"github.com/lrstanley/girc"
)

// A refused send is logged, and a failed join still reaches the fatal channel-error handler.
func TestSendErrorsRouting(t *testing.T) {
	r := NewRegistry()
	r.Register(&ChannelErrorBehavior{})
	r.Register(&SendErrorBehavior{})
	if got := r.behaviors[girc.ERR_CANNOTSENDTOCHAN][0].Name(); got != "send_error" {
		t.Errorf("404 handled by %s", got)
	}
	if got := r.behaviors[girc.ERR_NOSUCHCHANNEL][0].Name(); got != "channel_error" {
		t.Errorf("403 handled by %s", got)
	}
}
