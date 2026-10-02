// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package commands

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode"

	"B4reMetal/metald/internal/irc"
)

// NowPlayingCommand says what the web radio (plugins/radio.py, contrib/radio) is playing. It reads the
// radio tool's settings, RADIO_API_URL and RADIO_PAGE_URL.
type NowPlayingCommand struct{}

func (c *NowPlayingCommand) Name() string    { return "+np" }
func (c *NowPlayingCommand) AdminOnly() bool { return false }

var radioClient = &http.Client{Timeout: 5 * time.Second}

type nowPlaying struct {
	Title     string `json:"title"`
	Listeners *int   `json:"listeners"`
}

func (c *NowPlayingCommand) Execute(ctx irc.ChatContextInterface) {
	api, page := strings.TrimRight(os.Getenv("RADIO_API_URL"), "/"), os.Getenv("RADIO_PAGE_URL")
	if api == "" {
		ctx.Reply("there is no radio set up")
		return
	}
	resp, err := radioClient.Get(api + "/now")
	if err != nil {
		ctx.GetLogger().Warn("radio_unreachable", "error", err.Error())
		ctx.Reply("the radio is unavailable right now")
		return
	}
	defer resp.Body.Close()
	var np nowPlaying
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&np) != nil {
		ctx.GetLogger().Warn("radio_bad_reply", "status", resp.StatusCode)
		ctx.Reply("the radio is unavailable right now")
		return
	}
	ctx.Reply(formatNowPlaying(np, page))
}

func formatNowPlaying(np nowPlaying, page string) string {
	title := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, np.Title)
	if len([]rune(title)) > 120 {
		title = string([]rune(title)[:120]) + "…"
	}
	parts := []string{"nothing is playing"}
	if title != "" {
		parts = []string{"now playing: " + title}
	}
	if np.Listeners != nil {
		parts = append(parts, fmt.Sprintf("%d listening", *np.Listeners))
	}
	if page != "" {
		parts = append(parts, page)
	}
	return strings.Join(parts, " · ")
}
