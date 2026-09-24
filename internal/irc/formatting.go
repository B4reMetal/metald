// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package irc

import (
	"regexp"
	"sort"
	"strings"
)

// Real IRC formatting control codes (mIRC-style, universally supported by IRC clients).
const (
	ircBold      = "\x02"
	ircItalic    = "\x1D"
	ircUnderline = "\x1F"
	ircColor     = "\x03"
	ircColorEnd  = "\x03" // bare \x03 resets color only, leaves bold/underline alone
)

// ircColorNames maps friendly color names to mIRC color codes.
var ircColorNames = map[string]string{
	"white":   "00",
	"black":   "01",
	"blue":    "02",
	"navy":    "02",
	"green":   "03",
	"red":     "04",
	"brown":   "05",
	"maroon":  "05",
	"purple":  "06",
	"orange":  "07",
	"olive":   "07",
	"yellow":  "08",
	"lime":    "09",
	"teal":    "10",
	"cyan":    "11",
	"royal":   "12",
	"pink":    "13",
	"fuchsia": "13",
	"grey":    "14",
	"gray":    "14",
	"silver":  "15",
}

var (
	tagColor     = regexp.MustCompile(`(?is)\[color=([a-z]+)\](.*?)\[/color\]`)
	tagBold      = regexp.MustCompile(`(?is)\[b\](.*?)\[/b\]`)
	tagItalic    = regexp.MustCompile(`(?is)\[i\](.*?)\[/i\]`)
	tagUnderline = regexp.MustCompile(`(?is)\[u\](.*?)\[/u\]`)
)

// Repairs for two ways the model garbles its own tags: a stray '>' where ']' was meant
// ("[/color>") and a dropped ']' after the opening tag name.
var (
	repairStrayCloseAngle = regexp.MustCompile(`\[(/?)(b|i|u)>`)
	repairColorCloseAngle = regexp.MustCompile(`(?i)\[(/?)color(=[a-z]+)?>`)
	repairMissingBracket  = regexp.MustCompile(`\[([^\[\]]+)\[/(b|i|u)\]`) // group 1 = body, group 2 = tag from the closer

	// A closer that names the colour ("[color=red]x[/red]") is repaired; a well-formed
	// "[/color]" is never touched.
	repairColorMismatchedClose = regexp.MustCompile(`(?is)\[color=([a-z]+)\](.*?)\[/([a-z]+)\]`)

	// Bold must run before italic so "**x**" isn't seen as two "*x*" pairs.
	mdBold   = regexp.MustCompile(`(?s)(\[)?\*\*(.+?)\*\*(\])?`)
	mdItalic = regexp.MustCompile(`(?s)(\[)?\*([^*\n]+?)\*(\])?`)
	// mdCodeFence must run before mdInlineCode, which cannot span a multi-line fence.
	mdCodeFence  = regexp.MustCompile("(?s)```[a-zA-Z0-9]*\n?(.*?)```")
	mdInlineCode = regexp.MustCompile("(?s)`([^`\n]+?)`") // no clean IRC equivalent - just drop the backticks

	mdLink = regexp.MustCompile(`\[([^\[\]]*)\]\((https?://[^\s()]+)\)`)

	repairMismatchedClose = regexp.MustCompile(`(?is)\[(b|i|u)\]([^\[\]]*)\[/(?:b|i|u|color(?:=[a-z]+)?)\]`)
	// Same, with a color opener closed by something else.
	repairColorMismatchedOpen = regexp.MustCompile(`(?is)\[color=([a-z]+)\]([^\[\]]*)\[/(?:b|i|u)\]`)

	colorOpen = regexp.MustCompile(`(?i)\[color=[a-z]+\]`)

	openBold       = regexp.MustCompile(`(?i)\[b\]`)
	closeBold      = regexp.MustCompile(`(?i)\[/b\]`)
	openItalic     = regexp.MustCompile(`(?i)\[i\]`)
	closeItalic    = regexp.MustCompile(`(?i)\[/i\]`)
	openUnderline  = regexp.MustCompile(`(?i)\[u\]`)
	closeUnderline = regexp.MustCompile(`(?i)\[/u\]`)

	bareColorTag = regexp.MustCompile(`(?is)\[(` + colorNamePattern + `)\](.*?)\[/(` + colorNamePattern + `)\]`)
)

// colorNamePattern is the alternation of known colour names, built once from
// ircColorNames so the two cannot drift apart.
var colorNamePattern = buildColorNamePattern()

func buildColorNamePattern() string {
	names := make([]string, 0, len(ircColorNames))
	for name := range ircColorNames {
		names = append(names, name)
	}
	sort.Strings(names) // deterministic, and longest-first is not needed for alternation of distinct words
	return strings.Join(names, "|")
}

// repairUnclosedFormat closes a [b]/[i]/[u] that never got its closing tag, applying the formatting
// from the open tag to the end of the message.
func repairUnclosedFormat(s string) string {
	for _, tag := range []struct {
		open, close *regexp.Regexp
		name        string
	}{
		{openBold, closeBold, "b"},
		{openItalic, closeItalic, "i"},
		{openUnderline, closeUnderline, "u"},
	} {
		missing := len(tag.open.FindAllString(s, -1)) - len(tag.close.FindAllString(s, -1))
		for i := 0; i < missing; i++ {
			s += "[/" + tag.name + "]"
		}
	}
	return s
}

// repairBareColorTag rewrites "[blue]x[/blue]" into the real syntax, but only when both halves name
// the SAME known colour - so an unrelated bracketed word is never mistaken for formatting.
func repairBareColorTag(s string) string {
	return bareColorTag.ReplaceAllStringFunc(s, func(m string) string {
		sub := bareColorTag.FindStringSubmatch(m)
		if !strings.EqualFold(sub[1], sub[3]) {
			return m
		}
		return "[color=" + sub[1] + "]" + sub[2] + "[/color]"
	})
}

// repairUnclosedColor closes a [color=x] that never got its [/color].
func repairUnclosedColor(s string) string {
	opens := colorOpen.FindAllStringIndex(s, -1)
	if len(opens) == 0 {
		return s
	}

	var b strings.Builder
	for i, loc := range opens {
		// Text before this open (for the first open only; later iterations
		// start where the previous segment ended).
		if i == 0 {
			b.WriteString(s[:loc[0]])
		}
		// The segment runs from this open until the next one, or end.
		end := len(s)
		if i+1 < len(opens) {
			end = opens[i+1][0]
		}
		segment := s[loc[0]:end]

		b.WriteString(segment)
		// Only add a close if this segment doesn't already have one.
		if !strings.Contains(strings.ToLower(segment), "[/color]") {
			b.WriteString("[/color]")
		}
	}
	return b.String()
}

func convertMarkdownEmphasis(re *regexp.Regexp, tag, s string) string {
	return re.ReplaceAllStringFunc(s, func(m string) string {
		sub := re.FindStringSubmatch(m)
		open, body, close := sub[1], sub[2], sub[3]
		if open == "" || close == "" {
			// Not a genuine matched pair - keep whichever bracket showed up,
			// it wasn't part of the emphasis wrapping.
			return open + "[" + tag + "]" + body + "[/" + tag + "]" + close
		}
		return "[" + tag + "]" + body + "[/" + tag + "]"
	})
}

func repairMalformedTags(s string) string {
	s = mdCodeFence.ReplaceAllString(s, "$1") // before bold/italic - fence content shouldn't be re-parsed as emphasis
	// Links before emphasis: a label like "[*x*](url)" should have its
	// brackets consumed here, not treated as bracket-wrapped emphasis.
	s = mdLink.ReplaceAllString(s, "$2")
	s = convertMarkdownEmphasis(mdBold, "b", s)
	s = convertMarkdownEmphasis(mdItalic, "i", s)
	s = mdInlineCode.ReplaceAllString(s, "$1")
	// Mismatched open/close pairs, trusting the opening tag.
	s = repairMismatchedClose.ReplaceAllString(s, "[$1]$2[/$1]")
	s = repairColorMismatchedOpen.ReplaceAllString(s, "[color=$1]$2[/color]")
	s = repairStrayCloseAngle.ReplaceAllString(s, "[${1}${2}]")
	s = repairColorCloseAngle.ReplaceAllString(s, "[${1}color${2}]")
	s = repairMissingBracket.ReplaceAllStringFunc(s, func(m string) string {
		sub := repairMissingBracket.FindStringSubmatch(m)
		body, tag := sub[1], sub[2]
		return "[" + tag + "]" + body + "[/" + tag + "]"
	})
	s = repairColorMismatchedClose.ReplaceAllStringFunc(s, func(m string) string {
		sub := repairColorMismatchedClose.FindStringSubmatch(m)
		colorName, body, closer := sub[1], sub[2], sub[3]
		if !strings.EqualFold(closer, colorName) {
			return m
		}
		return "[color=" + colorName + "]" + body + "[/color]"
	})

	// Bare colour names ("[blue]x[/blue]") become real colour tags before the
	// unclosed-colour sweep, so a repaired run is not then seen as unclosed.
	s = repairBareColorTag(s)

	s = repairUnclosedColor(s)
	s = repairUnclosedFormat(s)
	return s
}

// RenderIRCFormatting converts [b]/[i]/[u]/[color=name] tags into real IRC formatting control
// codes.
func RenderIRCFormatting(s string) string {
	s = repairMalformedTags(s)
	s = tagColor.ReplaceAllStringFunc(s, func(m string) string {
		sub := tagColor.FindStringSubmatch(m)
		code, ok := ircColorNames[strings.ToLower(sub[1])]
		if !ok {
			return sub[2]
		}
		return ircColor + code + sub[2] + ircColorEnd
	})
	s = tagBold.ReplaceAllString(s, ircBold+"$1"+ircBold)
	s = tagItalic.ReplaceAllString(s, ircItalic+"$1"+ircItalic)
	s = tagUnderline.ReplaceAllString(s, ircUnderline+"$1"+ircUnderline)
	return s
}
