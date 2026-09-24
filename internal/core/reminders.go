// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Reminder bounds.
const (
	MinReminderDelay = time.Minute
	MaxReminderDelay = 72 * time.Hour // three days

	// Anyone in the channel can get the model to set one of these, so the
	// store is capped to stop it being used as a delayed flood cannon.
	MaxRemindersPerNick = 5
	MaxRemindersTotal   = 50

	MaxReminderLateness = 24 * time.Hour
)

// Reminder is one pending scheduled message.
type Reminder struct {
	ID      string    `json:"id"`
	Network string    `json:"network"` // which network's connection delivers it
	Nick    string    `json:"nick"`    // who gets pinged
	Channel string    `json:"channel"` // where it fires
	Text    string    `json:"text"`
	Due     time.Time `json:"due"`
	SetBy   string    `json:"set_by"` // who asked for it
}

// ReminderStore holds pending reminders, persisted to disk.
type ReminderStore struct {
	mu      sync.RWMutex
	path    string
	entries map[string]Reminder // id -> reminder
}

var (
	globalReminders     *ReminderStore
	globalRemindersOnce sync.Once
)

// Reminders returns the process-wide reminder store, loading persisted state
// on first use.
func Reminders() *ReminderStore {
	globalRemindersOnce.Do(func() {
		globalReminders = NewReminderStore(DataPath("reminders.json"))
	})
	return globalReminders
}

// NewReminderStore creates a store backed by path, loading existing state.
func NewReminderStore(path string) *ReminderStore {
	s := &ReminderStore{path: path, entries: make(map[string]Reminder)}
	s.load()
	return s
}

// ErrReminderLimit is returned when a store or per-nick cap is hit.
type ErrReminderLimit struct{ Reason string }

func (e ErrReminderLimit) Error() string { return e.Reason }

// Add schedules text for nick in channel, due after d.
func (s *ReminderStore) Add(network, nick, channel, text, setBy string, d time.Duration) (Reminder, error) {
	if d < MinReminderDelay {
		d = MinReminderDelay
	}
	if d > MaxReminderDelay {
		d = MaxReminderDelay
	}

	s.mu.Lock()
	if len(s.entries) >= MaxRemindersTotal {
		s.mu.Unlock()
		return Reminder{}, ErrReminderLimit{
			Reason: fmt.Sprintf("too many reminders pending (%d)", MaxRemindersTotal),
		}
	}
	count := 0
	for _, r := range s.entries {
		if strings.EqualFold(r.Nick, nick) {
			count++
		}
	}
	if count >= MaxRemindersPerNick {
		s.mu.Unlock()
		return Reminder{}, ErrReminderLimit{
			Reason: fmt.Sprintf("%s already has %d reminders pending", nick, MaxRemindersPerNick),
		}
	}

	r := Reminder{
		ID:      newReminderID(),
		Nick:    strings.TrimSpace(nick),
		Channel: channel,
		Text:    strings.TrimSpace(text),
		Due:     time.Now().Add(d),
		SetBy:   setBy,
	}
	s.entries[r.ID] = r
	s.mu.Unlock()

	s.save()
	return r, nil
}

// Cancel removes a reminder by id. Returns false if it wasn't there.
func (s *ReminderStore) Cancel(network, id string) bool {
	key := strings.ToLower(strings.TrimSpace(id))
	s.mu.Lock()
	existing, existed := s.entries[key]
	if existed && existing.Network != network {
		s.mu.Unlock()
		return false
	}
	delete(s.entries, key)
	s.mu.Unlock()
	if existed {
		s.save()
	}
	return existed
}

// List returns pending reminders sorted by due time, soonest first.
func (s *ReminderStore) List(network string) []Reminder {
	s.mu.RLock()
	out := make([]Reminder, 0, len(s.entries))
	for _, r := range s.entries {
		if r.Network != network {
			continue
		}
		out = append(out, r)
	}
	s.mu.RUnlock()

	sort.Slice(out, func(i, j int) bool { return out[i].Due.Before(out[j].Due) })
	return out
}

// PopDue atomically removes and returns every reminder due at or before now.
func (s *ReminderStore) PopDue(now time.Time, network string) (due []Reminder, stale []Reminder) {
	s.mu.Lock()
	for id, r := range s.entries {
		if r.Network != network {
			continue
		}
		if r.Due.After(now) {
			continue
		}
		if now.Sub(r.Due) > MaxReminderLateness {
			stale = append(stale, r)
		} else {
			due = append(due, r)
		}
		delete(s.entries, id)
	}
	s.mu.Unlock()

	if len(due) > 0 || len(stale) > 0 {
		s.save()
	}
	sort.Slice(due, func(i, j int) bool { return due[i].Due.Before(due[j].Due) })
	return due, stale
}

func newReminderID() string {
	b := make([]byte, 3)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// load reads persisted state. A missing or unreadable file just means
// nothing is scheduled.
func (s *ReminderStore) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var raw map[string]Reminder
	if err := json.Unmarshal(data, &raw); err != nil {
		GetLogger().Warn("reminder_store_unreadable", "path", s.path, "error", err.Error())
		return
	}
	for id, r := range raw {
		s.entries[id] = r
	}
}

// save writes state atomically (temp file + rename) so a crash mid-write
// can't truncate the file and lose every pending reminder.
func (s *ReminderStore) save() {
	s.mu.RLock()
	snapshot := make(map[string]Reminder, len(s.entries))
	for id, r := range s.entries {
		snapshot[id] = r
	}
	s.mu.RUnlock()

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		GetLogger().Error("reminder_store_marshal_failed", "error", err.Error())
		return
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		GetLogger().Error("reminder_store_write_failed", "path", tmp, "error", err.Error())
		return
	}
	if err := os.Rename(tmp, s.path); err != nil {
		GetLogger().Error("reminder_store_rename_failed", "path", s.path, "error", err.Error())
		_ = os.Remove(tmp)
	}
}

// ReminderSender delivers a due reminder to a channel.
type ReminderSender func(channel, message string) bool

// ReminderTick is how often the scheduler checks for due reminders.
const ReminderTick = 15 * time.Second

// RunReminderScheduler delivers reminders as they come due, until ctx ends.
func RunReminderScheduler(ctx context.Context, store *ReminderStore, network string, send ReminderSender) {
	ticker := time.NewTicker(ReminderTick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			deliverDueReminders(store, network, send)
		}
	}
}

func deliverDueReminders(store *ReminderStore, network string, send ReminderSender) {
	due, stale := store.PopDue(time.Now(), network)

	for _, r := range stale {
		GetLogger().Warn("reminder_dropped_stale",
			"id", r.ID, "nick", r.Nick, "due", r.Due.UTC(), "text", r.Text)
	}

	for _, r := range due {
		msg := fmt.Sprintf("%s: %s", r.Nick, r.Text)
		// Late delivery is called out rather than hidden - the bot restarts often, so "this was supposed
		// to reach you earlier" is honest and stops a stale reminder reading as a current one.
		if late := time.Since(r.Due); late > 2*ReminderTick {
			msg = fmt.Sprintf("%s (late by %s, i was restarted)", msg, late.Round(time.Minute))
		}

		if !send(r.Channel, msg) {
			// Couldn't deliver - put it back so the next tick retries
			// rather than silently eating it.
			store.mu.Lock()
			store.entries[r.ID] = r
			store.mu.Unlock()
			store.save()
			continue
		}

		GetLogger().Info("reminder_fired",
			"id", r.ID, "nick", r.Nick, "channel", r.Channel,
			"set_by", r.SetBy, "text", r.Text)
	}
}
