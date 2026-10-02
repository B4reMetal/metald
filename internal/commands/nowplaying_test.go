// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package commands

import (
	"net/http"
	"net/http/httptest"
	"testing"

	mocktest "B4reMetal/metald/internal/testing"
)

func TestNowPlaying(t *testing.T) {
	body := `{"title": "blue\u0003 hair", "by": "botty", "listeners": 3}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/now" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	t.Setenv("RADIO_API_URL", srv.URL+"/")
	t.Setenv("RADIO_PAGE_URL", "https://radio.example.com/")

	cases := []struct {
		body, want string
	}{
		{`{"title": "blue\u0003 hair", "listeners": 3}`, "now playing: blue hair · 3 listening · https://radio.example.com/"},
		{`{"listeners": 0}`, "nothing is playing · 0 listening · https://radio.example.com/"},
	}
	for _, c := range cases {
		body = c.body
		ctx := mocktest.NewMockContext().WithArgs("+np")
		(&NowPlayingCommand{}).Execute(ctx)
		if len(ctx.Replies) != 1 || ctx.Replies[0] != c.want {
			t.Errorf("+np = %q, want %q", ctx.Replies, c.want)
		}
	}

	t.Setenv("RADIO_API_URL", "http://127.0.0.1:1")
	ctx := mocktest.NewMockContext().WithArgs("+np")
	(&NowPlayingCommand{}).Execute(ctx)
	if len(ctx.Replies) != 1 || ctx.Replies[0] != "the radio is unavailable right now" {
		t.Errorf("unreachable radio = %q", ctx.Replies)
	}
}
