// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package commands

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mocktest "B4reMetal/metald/internal/testing"
)

func TestSkip(t *testing.T) {
	title, skips := `blue\u0003 hair`, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/now":
			_, _ = w.Write([]byte(`{"title": "` + title + `"}`))
		case r.URL.Path == "/skip" && r.Method == http.MethodPost && r.Header.Get("Authorization") == "Bearer t0k":
			skips++
			_, _ = w.Write([]byte(`{"skipped": true}`))
		default:
			http.Error(w, "no", http.StatusUnauthorized)
		}
	}))
	defer srv.Close()
	t.Setenv("RADIO_API_URL", srv.URL)
	t.Setenv("RADIO_TOKEN", "t0k")
	lastSkip = time.Time{}

	run := func() string {
		ctx := mocktest.NewMockContext().WithArgs("+skip")
		(&SkipCommand{}).Execute(ctx)
		if len(ctx.Replies) != 1 {
			t.Fatalf("replies = %q", ctx.Replies)
		}
		return ctx.Replies[0]
	}
	if got := run(); got != "skipped blue hair" || skips != 1 {
		t.Errorf("first +skip = %q (skips %d)", got, skips)
	}
	if got := run(); !strings.HasPrefix(got, "someone just skipped") || skips != 1 {
		t.Errorf("+skip inside the cooldown = %q (skips %d)", got, skips)
	}

	lastSkip, title = time.Time{}, ""
	if got := run(); got != "nothing is playing" || skips != 1 {
		t.Errorf("+skip with nothing playing = %q (skips %d)", got, skips)
	}

	t.Setenv("RADIO_TOKEN", "wrong")
	title = "x"
	if got := run(); got != "the radio is unavailable right now" || !lastSkip.IsZero() {
		t.Errorf("refused skip = %q; a failed skip must not start the cooldown", got)
	}
}
