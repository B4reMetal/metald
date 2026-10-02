// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package llm

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alexschlessinger/pollytool/messages"

	"B4reMetal/metald/internal/core"
	"B4reMetal/metald/internal/irc"
	mocktest "B4reMetal/metald/internal/testing"
)

func TestTaskConfigCarriesTheTaskBudget(t *testing.T) {
	cfg := mocktest.DefaultTestConfig()
	cfg.Session.TaskMaxTime = 45 * time.Minute
	cfg.Session.TaskMaxIterations = 40
	cfg.Bot.ShowToolActions = true
	tc := taskConfig(cfg, 45*time.Minute)
	if tc.API.Timeout != 45*time.Minute || tc.Session.MaxIterations != 40 || tc.Bot.ShowToolActions {
		t.Errorf("task config = timeout %v, iterations %d, announcements %v", tc.API.Timeout, tc.Session.MaxIterations, tc.Bot.ShowToolActions)
	}
	if cfg.API.Timeout == 45*time.Minute || cfg.Session.MaxIterations == 40 || !cfg.Bot.ShowToolActions {
		t.Error("the shared config was changed")
	}
	cfg.Model.MaxTokens, cfg.Session.TaskMaxTokens = 32768, 65536
	if got := taskConfig(cfg, time.Minute).Model.MaxTokens; got != 65536 || cfg.Model.MaxTokens != 32768 {
		t.Errorf("task output limit = %d (chat's now %d), want 65536 for tasks and chat unchanged", got, cfg.Model.MaxTokens)
	}
}

func TestPrepareTaskSessionAddsTaskPromptAndDetectsResume(t *testing.T) {
	sys := mocktest.NewMockSystem()
	session, _ := sys.SessionStore.Get("task/net/1")
	ctx := mocktest.NewMockContext().WithSystem(sys).WithSession(session)
	cfg := ctx.GetConfig()

	if prepareTaskSession(ctx, cfg) {
		t.Error("a fresh task reported as resumed")
	}
	h := session.GetHistory()
	if !strings.Contains(h[0].Content, cfg.Bot.TaskPrompt) {
		t.Errorf("task prompt missing from the system message: %q", h[0].Content)
	}
	session.AddMessage(messages.ChatMessage{Role: messages.MessageRoleUser, Content: "(nick:bob) do it"})
	if !prepareTaskSession(ctx, cfg) {
		t.Error("earlier work not detected")
	}
	if n := len(session.GetHistory()); n != 2 {
		t.Errorf("history = %d messages, want system + the earlier turn", n)
	}
}

func TestStartTaskGuards(t *testing.T) {
	sys := mocktest.NewMockSystem()
	chanSession, _ := sys.SessionStore.Get("net/#test")

	screened := mocktest.NewMockContext().WithSystem(sys).WithSession(chanSession).WithSource("mallory")
	screened.GetConfig().Bot.ScreenNicks = []string{"mallory"}
	if _, err := irc.StartWork(screened, core.TaskSpec{Objective: "job"}); !errors.Is(err, core.ErrTaskQuota) {
		t.Errorf("screened nick started a task: %v", err)
	}

	private := mocktest.NewMockContext().WithSystem(sys).WithSession(chanSession).WithPrivate(true)
	if _, err := irc.StartWork(private, core.TaskSpec{Objective: "job"}); !errors.Is(err, core.ErrTaskQuota) {
		t.Errorf("private message started a task: %v", err)
	}

	taskSession, _ := sys.SessionStore.Get("task/net/99")
	nested := mocktest.NewMockContext().WithSystem(sys).WithSession(taskSession).WithAdmin(true)
	if _, err := irc.StartWork(nested, core.TaskSpec{Objective: "job"}); !errors.Is(err, core.ErrTaskQuota) {
		t.Errorf("a task started another task: %v", err)
	}

	ok := mocktest.NewMockContext().WithSystem(sys).WithSession(chanSession).WithSource("erin")
	task, err := irc.StartWork(ok, core.TaskSpec{Objective: "write a haiku about toads"})
	if err != nil || task.Channel != "#test" || task.OwnerMask != "erin!~u@test.host" {
		t.Errorf("task = %+v, err %v", task, err)
	}
}

func TestInlineResultKeepsProseAndPastesCode(t *testing.T) {
	lines := []string{"(nick:erin) here's the function:", "```python", "def f():", "    return 1", "```", "it returns one."}
	shown, cut := inlineResult(lines)
	if !cut || strings.Join(shown, "|") != "here's the function:|it returns one." {
		t.Errorf("shown %q cut %v", shown, cut)
	}
	short := []string{"a four-line poem", "line two"}
	if shown, cut := inlineResult(short); cut || len(shown) != 2 {
		t.Errorf("short prose: shown %q cut %v", shown, cut)
	}
	long := make([]string, 10)
	for i := range long {
		long[i] = "line"
	}
	if shown, cut := inlineResult(long); !cut || len(shown) != taskInlineLines {
		t.Errorf("long prose: %d shown, cut %v", len(shown), cut)
	}
}

// The CHIP-8 goal's gist link was its 8th line and only reached the channel inside a paste.
func TestInlineResultAlwaysShowsLinks(t *testing.T) {
	lines := []string{"one", "two", "three", "four", "five", "six", "seven",
		"the code: https://gist.example.com/alice/a2c24738", "eight"}
	shown, cut := inlineResult(lines)
	if !cut || shown[len(shown)-1] != "the code: https://gist.example.com/alice/a2c24738" || len(shown) != taskInlineLines+1 {
		t.Errorf("shown %q cut %v", shown, cut)
	}
}
