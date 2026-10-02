// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import "time"

// Recap returns the running summary of a conversation's older turns, or "".
func (c *ContextDB) Recap(key string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var recap string
	if err := c.db.QueryRow(`SELECT recap FROM recaps WHERE key = ?`, key).Scan(&recap); err != nil {
		return ""
	}
	return recap
}

// SetRecap replaces a conversation's recap.
func (c *ContextDB) SetRecap(key, recap string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.db.Exec(
		`INSERT INTO recaps(key, recap, updated) VALUES(?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET recap = excluded.recap, updated = excluded.updated`,
		key, recap, time.Now().Unix())
	return err
}

// ClearRecap deletes a conversation's recap and reports whether there was one.
func (c *ContextDB) ClearRecap(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	res, err := c.db.Exec(`DELETE FROM recaps WHERE key = ?`, key)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}
