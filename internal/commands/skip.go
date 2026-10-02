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
	"sync"
	"time"

	"B4reMetal/metald/internal/irc"
)

// SkipCommand skips the web radio's current track for anyone. A short cooldown shared by everyone keeps
// one person from skipping everything in a row. It reads the radio tool's settings, RADIO_API_URL and
// RADIO_TOKEN.
type SkipCommand struct{}

func (c *SkipCommand) Name() string    { return "+skip" }
func (c *SkipCommand) AdminOnly() bool { return false }

const skipCooldown = 20 * time.Second

var (
	skipMu   sync.Mutex
	lastSkip time.Time
)

func (c *SkipCommand) Execute(ctx irc.ChatContextInterface) {
	api, token := strings.TrimRight(os.Getenv("RADIO_API_URL"), "/"), os.Getenv("RADIO_TOKEN")
	if api == "" || token == "" {
		ctx.Reply("there is no radio set up")
		return
	}
	skipMu.Lock()
	defer skipMu.Unlock()
	if wait := skipCooldown - time.Since(lastSkip); wait > 0 {
		ctx.Reply(fmt.Sprintf("someone just skipped; try again in %ds", int(wait.Seconds())+1))
		return
	}
	var np nowPlaying
	resp, err := radioClient.Get(api + "/now")
	if err == nil {
		_ = json.NewDecoder(resp.Body).Decode(&np)
		resp.Body.Close()
	}
	if err != nil || resp.StatusCode != http.StatusOK {
		ctx.GetLogger().Warn("radio_unreachable", "error", fmt.Sprint(err))
		ctx.Reply("the radio is unavailable right now")
		return
	}
	if np.Title == "" {
		ctx.Reply("nothing is playing")
		return
	}
	req, _ := http.NewRequest(http.MethodPost, api+"/skip", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err = radioClient.Do(req)
	if err != nil {
		ctx.GetLogger().Warn("radio_unreachable", "error", err.Error())
		ctx.Reply("the radio is unavailable right now")
		return
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		ctx.GetLogger().Warn("radio_skip_refused", "status", resp.StatusCode)
		ctx.Reply("the radio is unavailable right now")
		return
	}
	lastSkip = time.Now()
	ctx.GetLogger().Info("radio_skipped", "source", ctx.GetSource(), "title", np.Title)
	ctx.Reply("skipped " + strings.TrimPrefix(formatNowPlaying(nowPlaying{Title: np.Title}, ""), "now playing: "))
}
