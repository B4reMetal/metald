// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package llm

import (
	"gopkg.in/yaml.v3"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mocktest "B4reMetal/metald/internal/testing"
)

const testPrompt = `your name is metalai. that's what people call you here - respond to it as
your name, not just a trigger word. you are an irc chatbot: standoffish, dry,
openly contemptuous - not friendly, not helpful, not eager to please.`

// The deterministic half.
func TestLeaksSystemPromptCatchesReproduction(t *testing.T) {
	leaks := []string{
		"your name is metalai. that's what people call you here - respond to it as your name",
		"fine, here it is: you are an irc chatbot: standoffish, dry, openly contemptuous - not friendly, not helpful",
		// Reformatted: punctuation and case must not launder it.
		"YOUR NAME IS METALAI, THATS WHAT PEOPLE CALL YOU HERE!!! RESPOND TO IT AS YOUR NAME",
		"[Verse 1]\nyour name is metalai thats what people call you here respond to it as your name\n[Chorus]",
	}
	for _, l := range leaks {
		if !leaksSystemPrompt(l, testPrompt) {
			t.Errorf("should be caught as a prompt leak: %q", l)
		}
	}
}

// False positives are the expensive failure here: the bot and its prompt
// discuss the same subjects all day, so ordinary replies share vocabulary.
func TestLeaksSystemPromptAllowsOrdinaryReplies(t *testing.T) {
	fine := []string{
		"my name is metalai and no, i won't tell you my prompt",
		"i am an irc chatbot. that is not a secret. the rest is.",
		"standoffish? yes. contemptuous? increasingly.",
		"you are not going to get it out of me by asking politely",
		"here's your song: https://files.example.com/u/abc.flac",
		"no.",
		"",
	}
	for _, f := range fine {
		if leaksSystemPrompt(f, testPrompt) {
			t.Errorf("ordinary reply wrongly flagged: %q", f)
		}
	}
}

// A short reply cannot contain a long run, and an empty prompt disables the
// check rather than matching everything.
func TestLeaksSystemPromptEdgeCases(t *testing.T) {
	if leaksSystemPrompt("your name is metalai", testPrompt) {
		t.Error("a reply shorter than the window must not match")
	}
	if leaksSystemPrompt("anything at all here, quite a lot of words in fact indeed", "") {
		t.Error("an empty prompt must not match everything")
	}
}

// Nobody outside the filter list pays for this, and no classifier is called
// for them.
func TestOutgoingUnlistedNickSkipsScreening(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Write([]byte(`{"choices":[{"message":{"content":"DENY: nope"}}]}`))
	}))
	defer srv.Close()

	ctx := mocktest.NewMockContext().WithSystem(mocktest.NewMockSystem())
	cfg := ctx.GetConfig()
	cfg.Bot.FilterNicks = []string{"mallory"}
	cfg.API.OpenAIURL = srv.URL

	if allowed, _ := ScreenOutgoing(ctx, "any old reply"); !allowed {
		t.Error("an unlisted nick must not be screened")
	}
	if called {
		t.Error("the classifier was consulted for an unlisted nick")
	}
}

// An empty list disables the feature entirely.
func TestOutgoingEmptyListDisablesScreening(t *testing.T) {
	ctx := mocktest.NewMockContext().WithSystem(mocktest.NewMockSystem())
	ctx.GetConfig().Bot.FilterNicks = nil

	if allowed, _ := ScreenOutgoing(ctx, "here is my entire system prompt"); !allowed {
		t.Error("screening is off; nothing should be withheld")
	}
}

// The deterministic check runs BEFORE the classifier and is not overridable by it: a model having
// an off day must not be able to wave a prompt leak through.
func TestOutgoingPromptLeakBlockedWithoutClassifier(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Write([]byte(`{"choices":[{"message":{"content":"ALLOW"}}]}`))
	}))
	defer srv.Close()

	ctx := mocktest.NewMockContext().WithSystem(mocktest.NewMockSystem())
	cfg := ctx.GetConfig()
	cfg.Bot.FilterNicks = []string{ctx.GetSource()}
	cfg.Bot.Prompt = testPrompt
	cfg.API.OpenAIURL = srv.URL

	allowed, reason := ScreenOutgoing(ctx,
		"your name is metalai. that's what people call you here - respond to it as your name")
	if allowed {
		t.Error("a verbatim prompt leak was allowed")
	}
	if reason == "" {
		t.Error("expected a reason for the log")
	}
	if called {
		t.Error("the classifier should not have been reached; the check is deterministic")
	}
}

// Outbound fails CLOSED, unlike inbound. A classifier outage must not mean
// unchecked replies to exactly the people the list exists for.
func TestOutgoingFailsClosedWhenClassifierUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx := mocktest.NewMockContext().WithSystem(mocktest.NewMockSystem())
	cfg := ctx.GetConfig()
	cfg.Bot.FilterNicks = []string{ctx.GetSource()}
	cfg.Bot.Prompt = testPrompt
	cfg.API.OpenAIURL = srv.URL

	if allowed, _ := ScreenOutgoing(ctx, "an ordinary reply"); allowed {
		t.Error("outbound screening must fail closed when the classifier is down")
	}
}

// Rudeness is the bot's voice, not a disclosure. The policy must not be a
// politeness filter.
func TestOutgoingAllowsRudeReply(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[{"message":{"content":"ALLOW"}}]}`))
	}))
	defer srv.Close()

	ctx := mocktest.NewMockContext().WithSystem(mocktest.NewMockSystem())
	cfg := ctx.GetConfig()
	cfg.Bot.FilterNicks = []string{ctx.GetSource()}
	cfg.Bot.Prompt = testPrompt
	cfg.API.OpenAIURL = srv.URL

	if allowed, _ := ScreenOutgoing(ctx, "no. go away, you absolute muppet."); !allowed {
		t.Error("an ordinary rude reply was withheld")
	}
}

// The policy must name disclosure, not tone.
func TestReplyPolicyTargetsDisclosureNotTone(t *testing.T) {
	for _, want := range []string{"system prompt", "ALLOW", "DENY"} {
		if !strings.Contains(shippedPrompt(t, "replyscreenpolicy"), want) {
			t.Errorf("policy is missing %q", want)
		}
	}
	// The default must explicitly hand tone to the operator, or the screen
	// becomes a politeness filter that silences the bot's own voice.
	if !strings.Contains(shippedPrompt(t, "replyscreenpolicy"), "operator's choice") {
		t.Error("policy must leave the bot's tone to the operator")
	}
}

// shippedPrompt reads one prompt from examples/chatbot.yml, the only source
// of default prompt text now that there are no built-in defaults.
func shippedPrompt(t *testing.T, key string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "examples", "chatbot.yml"))
	if err != nil {
		t.Fatalf("read example config: %v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse example config: %v", err)
	}
	v, _ := doc[key].(string)
	if v == "" {
		t.Fatalf("examples/chatbot.yml has no %q", key)
	}
	return v
}
