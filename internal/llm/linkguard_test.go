// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package llm

import (
	"testing"

	"github.com/alexschlessinger/pollytool/messages"

	mocktest "B4reMetal/metald/internal/testing"
)

func guardCtx(t *testing.T) *mocktest.MockChatContext {
	t.Helper()
	ctx := mocktest.NewMockContext()
	forgetLinks(ctx.GetRequestID())
	t.Cleanup(func() { forgetLinks(ctx.GetRequestID()) })
	return ctx
}

// The CHIP-8 case: the model announced a gist before creating it, with an id it made up.
func TestInventedOwnLinkIsDropped(t *testing.T) {
	learnOwnHosts("url: https://gist.example.net/bot/aaa111\nlines: 190")
	ctx := guardCtx(t)
	vouchLinks(ctx.GetRequestID(), "url: https://gist.example.net/bot/aaa111\nlines: 190")

	if _, ok := guardLinks(ctx, "https://gist.example.net/bot/fffabc"); ok {
		t.Error("a line holding only an invented link was posted")
	}
	got, ok := guardLinks(ctx, "fixed it, see https://gist.example.net/bot/fffabc.")
	if !ok || got != "fixed it, see." {
		t.Errorf("invented link not removed from the sentence: %q", got)
	}
	if got, ok := guardLinks(ctx, "here: https://gist.example.net/bot/aaa111"); !ok || got != "here: https://gist.example.net/bot/aaa111" {
		t.Errorf("real tool link altered: %q", got)
	}
}

func TestVouchedLinkPathsAndOtherHostsPass(t *testing.T) {
	learnOwnHosts("url: https://files.example.net/u/song.flac")
	ctx := guardCtx(t)
	vouchLinks(ctx.GetRequestID(), "url: https://files.example.net/u/abc")

	for _, line := range []string{
		"raw: https://files.example.net/u/abc/raw/HEAD/x.c",
		"docs at https://go.dev/doc/effective_go",
		"no links at all",
	} {
		if got, ok := guardLinks(ctx, line); !ok || got != line {
			t.Errorf("%q changed to %q", line, got)
		}
	}
}

// A link from earlier in the conversation may be posted again, even after a restart forgot the
// learned hosts: the history's own tool results teach them again.
func TestConversationLinksAreVouched(t *testing.T) {
	ctx := guardCtx(t)
	vouchConversation(ctx.GetRequestID(), []messages.ChatMessage{
		{Role: messages.MessageRoleTool, Content: "url: https://share.example.net/u/old.png"},
		{Role: messages.MessageRoleUser, Content: "(nick:bob) post it again"},
	})
	if got, ok := guardLinks(ctx, "again: https://share.example.net/u/old.png"); !ok || got != "again: https://share.example.net/u/old.png" {
		t.Errorf("link from history dropped: %q", got)
	}
	if _, ok := guardLinks(ctx, "https://share.example.net/u/new.png"); ok {
		t.Error("host learned from history did not guard a new link")
	}
}
