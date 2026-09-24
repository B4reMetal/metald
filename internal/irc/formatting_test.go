// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package irc

import "testing"

func TestRenderIRCFormatting(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"bold", "[b]hi[/b]", "\x02hi\x02"},
		{"italic", "[i]hi[/i]", "\x1Dhi\x1D"},
		{"underline", "[u]hi[/u]", "\x1Fhi\x1F"},
		{"known color", "[color=red]hi[/color]", "\x0304hi\x03"},
		{"case insensitive tag", "[COLOR=Red]hi[/COLOR]", "\x0304hi\x03"},
		{"unknown color strips tags", "[color=mauve]hi[/color]", "hi"},
		{"plain text unaffected", "just plain text", "just plain text"},
		{"mixed formatting", "[b]bold[/b] and [color=blue]blue[/color]", "\x02bold\x02 and \x0302blue\x03"},
		{"unmatched opening tag now applies to end", "[b]no closing tag", "\x02no closing tag\x02"},
		{"empty string", "", ""},

		// Malformed tags the model produces (see repairMalformedTags).
		{"stray angle-bracket close", "[color=red]nice try[/color>.", "\x0304nice try\x03."},
		{"missing bracket after opening tag", "[baremetal[/b] is the name", "\x02baremetal\x02 is the name"},
		{"stray angle on bold/italic/underline", "[b>hi[/b>", "\x02hi\x02"},
		{
			"trusts the well-formed closing tag over a mismatched opening",
			"[bxyz[/i]",
			"\x1Dbxyz\x1D",
		},
		{"color closed with its own name instead of 'color'", "[color=yellow]sledgehammer[/yellow]", "\x0308sledgehammer\x03"},
		{"well-formed color tag untouched by that repair", "[color=red]hi[/color]", "\x0304hi\x03"},
		{"markdown bold converted", "**less dumb**", "\x02less dumb\x02"},
		{"markdown italic converted", "*less dumb*", "\x1Dless dumb\x1D"},
		{"markdown inline code stripped", "run `--n-cpu-moe 32` now", "run --n-cpu-moe 32 now"},
		{"markdown bold not mistaken for two italics", "**bold** then *italic*", "\x02bold\x02 then \x1Ditalic\x1D"},

		{"bracket-wrapped markdown bold", "the answer is [**32 days.**]", "the answer is \x0232 days.\x02"},
		{"bracket-wrapped markdown italic", "[*note*]", "\x1Dnote\x1D"},
		// A bracket on only one side is not wrapping and must survive; only a matched pair is stripped.
		{"orphan leading bracket preserved", "[**bold** unrelated", "[\x02bold\x02 unrelated"},
		{"orphan trailing bracket preserved", "**bold**] unrelated", "\x02bold\x02] unrelated"},

		// Triple-backtick fenced ascii art. mdInlineCode alone
		// can't span it (no-newline body), so it needs its own repair.
		{
			"triple-backtick code fence stripped",
			"here:\n```\n /\\_/\\\n( o.o )\n > ^ <\n```\ndone",
			"here:\n /\\_/\\\n( o.o )\n > ^ <\n\ndone",
		},
		{"code fence with language tag stripped", "```go\nfmt.Println(1)\n```", "fmt.Println(1)\n"},

		// Asked for "rainbow text" the model chained colour
		// opens and never closed any of them, so every tag leaked as text.
		{
			"unclosed colour chain is closed per run",
			"[color=red]the [color=orange]full [color=yellow]pride",
			"\x0304the \x03\x0307full \x03\x0308pride\x03",
		},
		{"single unclosed colour", "[color=red]oops", "\x0304oops\x03"},
		// Well-formed colour must not gain a spurious extra close.
		{"well-formed colour untouched by the unclosed repair", "[color=red]hi[/color] there", "\x0304hi\x03 there"},
		{"text before an unclosed colour is preserved", "look: [color=blue]this", "look: \x0302this\x03"},

		// Markdown link syntax. IRC has no link markup, so the
		// label is dropped and the raw (clickable) URL kept.
		{"markdown link keeps only the url", "[url](https://files.example.com/u/j7Fe9X.png)", "https://files.example.com/u/j7Fe9X.png"},
		{"markdown link with prose label", "see [the beach](https://x.example/a.png) now", "see https://x.example/a.png now"},
		{"bare url untouched", "https://files.example.com/u/abc.png", "https://files.example.com/u/abc.png"},
		// A bracketed non-link must not be eaten by the link repair.
		{"bracketed text without url left alone", "[not a link] (text)", "[not a link] (text)"},

		// Opening tag closed by a different tag. Both sides are
		// well-formed here, so the opener is trusted.
		{"italic closed with bold tag", "you're such a [i]picky bastard[/b].", "you're such a \x1Dpicky bastard\x1D."},
		{"bold closed with color tag", "it's [b]ugly[/color=red], just like his code.", "it's \x02ugly\x02, just like his code."},
		{"color closed with bold tag", "[color=red]nope[/b]", "\x0304nope\x03"},

		{"unclosed bold runs to end", "[b]saying it out loud made me forget.", "\x02saying it out loud made me forget.\x02"},
		{"unclosed italic runs to end", "[i]an aside", "\x1Dan aside\x1D"},
		{"unclosed underline runs to end", "[u]underlined", "\x1Funderlined\x1F"},
		{"well-formed bold gains no extra close", "[b]bold[/b] then plain", "\x02bold\x02 then plain"},
		{"one closed one open", "[b]first[/b] and [b]second", "\x02first\x02 and \x02second\x02"},
		{"unclosed bold alongside colour", "[b]loud [color=red]and red", "\x02loud \x0304and red\x03\x02"},

		// A colour written as a bare name rather than
		// "[color=blue]...[/color]".
		{"bare colour name tag", "[blue]kicking, apparently[/blue]", "\x0302kicking, apparently\x03"},
		{"bare colour name, unknown word left alone", "[banana]not a colour[/banana]", "[banana]not a colour[/banana]"},
		{"bracketed word with mismatched halves left alone", "[blue]text[/green]", "[blue]text[/green]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderIRCFormatting(tt.in)
			if got != tt.want {
				t.Errorf("RenderIRCFormatting(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
