// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"strings"
	"time"
	"unicode"
)

// ChatLine is one logged line of a conversation.
type ChatLine struct {
	Nick string
	Text string
	At   time.Time
}

// maxLoggedLine bounds one stored line.
const maxLoggedLine = 1000

// LogLine records a line said in a conversation.
func (c *ContextDB) LogLine(key, nick, text string, at time.Time) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if len(text) > maxLoggedLine {
		text = text[:maxLoggedLine]
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.db.Exec(`INSERT INTO chatlog(key, nick, text, at) VALUES(?, ?, ?, ?)`,
		key, strings.ToLower(nick), text, at.Unix())
	return err
}

// SearchLog finds lines in one conversation matching any of the query's words, best matches first.
// nick narrows to one speaker; since drops older lines.
func (c *ContextDB) SearchLog(key, query, nick string, since time.Time, limit int) ([]ChatLine, error) {
	match := FTSQuery(query)
	if match == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	q := `SELECT l.nick, l.text, l.at FROM chatlog_fts f JOIN chatlog l ON l.id = f.rowid
	      WHERE chatlog_fts MATCH ? AND l.key = ? AND l.at >= ?`
	args := []any{match, key, since.Unix()}
	if nick = strings.ToLower(strings.TrimSpace(nick)); nick != "" {
		q += ` AND l.nick = ?`
		args = append(args, nick)
	}
	q += ` ORDER BY bm25(chatlog_fts) LIMIT ?`
	args = append(args, limit)

	c.mu.Lock()
	defer c.mu.Unlock()
	rows, err := c.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChatLine
	for rows.Next() {
		var l ChatLine
		var at int64
		if err := rows.Scan(&l.Nick, &l.Text, &at); err != nil {
			return nil, err
		}
		l.At = time.Unix(at, 0)
		out = append(out, l)
	}
	return out, rows.Err()
}

// PruneLog deletes lines older than cutoff and returns how many went.
func (c *ContextDB) PruneLog(cutoff time.Time) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	res, err := c.db.Exec(`DELETE FROM chatlog WHERE at < ?`, cutoff.Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// FTSQuery turns free text into an FTS5 query that matches any of its words. Every word is quoted,
// so nothing in the text is read as FTS syntax.
func FTSQuery(text string) string {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	seen := map[string]bool{}
	var terms []string
	for _, w := range words {
		if len(w) < 2 || stopwords[w] || seen[w] {
			continue
		}
		seen[w] = true
		terms = append(terms, `"`+w+`"`)
		if len(terms) == 16 {
			break
		}
	}
	return strings.Join(terms, " OR ")
}

// stopwords are too common to say anything about what a line is about.
var stopwords = map[string]bool{
	"the": true, "and": true, "for": true, "are": true, "but": true, "not": true, "you": true,
	"your": true, "with": true, "this": true, "that": true, "have": true, "has": true, "was": true,
	"were": true, "what": true, "when": true, "where": true, "who": true, "how": true, "why": true,
	"can": true, "could": true, "would": true, "should": true, "will": true, "about": true,
	"from": true, "they": true, "them": true, "their": true, "there": true, "then": true,
	"than": true, "just": true, "like": true, "into": true, "out": true, "our": true, "its": true,
	"his": true, "her": true, "him": true, "she": true, "any": true, "all": true, "some": true,
	"did": true, "does": true, "doing": true, "been": true, "being": true, "also": true, "too": true,
	"very": true, "get": true, "got": true, "make": true, "made": true, "say": true, "said": true,
	"is": true, "it": true, "to": true, "of": true, "in": true, "on": true, "at": true, "be": true,
	"me": true, "my": true, "we": true, "us": true, "an": true, "or": true, "if": true, "so": true,
	"do": true, "no": true, "yes": true, "up": true, "as": true, "by": true, "i": true, "a": true,
	"now": true, "here": true, "want": true, "need": true, "know": true, "think": true, "thing": true,
	"things": true, "more": true, "much": true, "many": true, "really": true, "again": true,
	"still": true, "even": true, "well": true, "good": true, "great": true, "new": true, "old": true,
	"one": true, "two": true, "first": true, "last": true, "time": true, "today": true, "day": true,
	"yeah": true, "lol": true, "lmao": true, "okay": true, "ok": true, "sure": true, "thanks": true,
	"thank": true, "hey": true, "hi": true, "hello": true, "please": true, "gonna": true,
	"wanna": true, "im": true, "dont": true, "cant": true, "youre": true, "thats": true,
	"whats": true, "use": true, "using": true, "try": true, "work": true, "works": true,
	"give": true, "show": true, "tell": true, "write": true, "create": true, "generate": true,
	"let": true, "put": true, "take": true, "see": true, "look": true, "go": true, "going": true,
	"only": true, "every": true, "same": true, "other": true, "which": true, "these": true,
	"those": true, "after": true, "before": true, "over": true, "under": true, "off": true,
	"way": true, "back": true, "down": true, "right": true, "left": true, "man": true,
}
