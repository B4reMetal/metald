// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func testTasks(t *testing.T) *TaskStore {
	t.Helper()
	s, err := NewTaskStore(testContextDB(t))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

var lim = TaskLimits{MaxActive: 1, DailyLimit: 3}

func job(objective string) TaskSpec { return TaskSpec{Kind: KindTask, Objective: objective} }

func TestTaskQuotaForNonAdmins(t *testing.T) {
	s := testTasks(t)
	if _, err := s.Start("net", "#c", "Bob", "bob!u@h", job("write a haiku"), false, lim); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start("net", "#c", "bob", "bob!u@h", job("another"), false, lim); !errors.Is(err, ErrTaskQuota) {
		t.Errorf("second active task allowed: %v", err)
	}
	for i := 0; i < 5; i++ {
		if _, err := s.Start("net", "#c", "admin", "admin!u@h", job("job"), true, lim); err != nil {
			t.Fatalf("admin refused: %v", err)
		}
	}
	if _, err := s.Start("other", "#c", "bob", "bob!u@h", job("job"), false, lim); err != nil {
		t.Errorf("quota leaked across networks: %v", err)
	}
}

func TestTaskDailyLimitCountsFinishedTasks(t *testing.T) {
	s := testTasks(t)
	for i := 0; i < 3; i++ {
		task, err := s.Start("net", "#c", "carol", "carol!u@h", job("job"), false, lim)
		if err != nil {
			t.Fatalf("task %d: %v", i, err)
		}
		s.Finish(task.ID, TaskDone, "ok", time.Second)
	}
	if _, err := s.Start("net", "#c", "carol", "carol!u@h", job("job"), false, lim); !errors.Is(err, ErrTaskQuota) {
		t.Errorf("fourth task in a day allowed: %v", err)
	}
	if _, err := s.Start("net", "#c", "dave", "dave!u@h", job("job"), false, TaskLimits{MaxActive: 1}); !errors.Is(err, ErrTaskQuota) {
		t.Errorf("daily limit 0 should make background work admin-only: %v", err)
	}
}

func TestTaskLifecycle(t *testing.T) {
	s := testTasks(t)
	a, _ := s.Start("net", "#c", "a", "a!u@h", job("first"), true, lim)
	b, _ := s.Start("net", "#c", "b", "b!u@h", job("second"), true, lim)

	got, ok := s.Claim("net")
	if !ok || got.ID != a.ID || got.Status != TaskRunning {
		t.Fatalf("claim = %+v, %v; want the oldest due task, running", got, ok)
	}
	if !s.Cancel("net", b.ID) {
		t.Fatal("queued task not cancellable")
	}
	if _, ok := s.Claim("net"); ok {
		t.Error("a cancelled task was claimed")
	}
	s.Finish(a.ID, TaskDone, "the haiku", 3*time.Second)
	if t1, _ := s.Get("net", a.ID); t1.Status != TaskDone || t1.Result != "the haiku" || t1.Runs != 1 || t1.Spent != 3*time.Second {
		t.Errorf("finished task = %+v", t1)
	}
	if s.Cancel("net", a.ID) {
		t.Error("a finished task was cancelled")
	}
	s.Finish(b.ID, TaskDone, "late", 0)
	if t2, _ := s.Get("net", b.ID); t2.Status != TaskCancelled {
		t.Errorf("finish overwrote a cancellation: %+v", t2)
	}
}

func TestTaskRequeueAfterRestart(t *testing.T) {
	s := testTasks(t)
	task, _ := s.Start("net", "#c", "a", "a!u@h", job("long job"), true, lim)
	s.Claim("net")
	if n := s.Requeue("net"); n != 1 {
		t.Fatalf("requeued %d, want 1", n)
	}
	got, ok := s.Claim("net")
	if !ok || got.ID != task.ID {
		t.Errorf("interrupted task not claimable again: %+v %v", got, ok)
	}
}

func TestTaskPauseAndList(t *testing.T) {
	s := testTasks(t)
	for _, o := range []string{"one", "two", "three"} {
		task, _ := s.Start("net", "#c", "a", "a!u@h", job(o), true, lim)
		if o != "three" {
			s.Finish(task.ID, TaskDone, "", 0)
		}
	}
	s.Pause("net")
	if _, ok := s.Claim("net"); ok {
		t.Error("claimed while paused")
	}
	s.Resume("net")
	list, err := s.List("net", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Objective != "three" || list[1].Objective != "two" {
		t.Errorf("list = %+v; want the open task, then the newest finished one", list)
	}
}

// A scheduled job is not claimed before its time.
func TestScheduleWaitsForItsTime(t *testing.T) {
	s := testTasks(t)
	later, _ := s.Start("net", "#c", "a", "a!u@h",
		TaskSpec{Kind: KindSchedule, Objective: "later", NextRun: time.Now().Add(time.Hour)}, true, lim)
	now, _ := s.Start("net", "#c", "a", "a!u@h", TaskSpec{Kind: KindSchedule, Objective: "now"}, true, lim)
	got, ok := s.Claim("net")
	if !ok || got.ID != now.ID {
		t.Fatalf("claim = %+v %v, want the due one", got, ok)
	}
	if _, ok := s.Claim("net"); ok {
		t.Error("a schedule was claimed before its time")
	}
	s.Reschedule(later.ID, time.Now().Add(-time.Second), "", 0)
	if got, ok := s.Claim("net"); !ok || got.ID != later.ID {
		t.Errorf("rescheduled job not claimed once due: %+v %v", got, ok)
	}
}

// Repeating schedules are admin-only and cannot run more often than the minimum interval.
func TestRecurringScheduleRules(t *testing.T) {
	s := testTasks(t)
	every := TaskSpec{Kind: KindSchedule, Objective: "news", Interval: time.Hour}
	if _, err := s.Start("net", "#c", "bob", "bob!u@h", every, false, lim); !errors.Is(err, ErrTaskQuota) {
		t.Errorf("non-admin set up a repeating schedule: %v", err)
	}
	fast := TaskSpec{Kind: KindSchedule, Objective: "spam", Interval: time.Minute}
	if _, err := s.Start("net", "#c", "admin", "admin!u@h", fast, true, lim); !errors.Is(err, ErrTaskQuota) {
		t.Errorf("repeat every minute allowed: %v", err)
	}
	if _, err := s.Start("net", "#c", "admin", "admin!u@h", every, true, lim); err != nil {
		t.Errorf("admin hourly schedule refused: %v", err)
	}
}

// A proposed goal waits for acceptance, counts toward the quota, and expires if nobody accepts it.
func TestGoalProposals(t *testing.T) {
	s := testTasks(t)
	goal := TaskSpec{Kind: KindGoal, Objective: "gcd", Criteria: "5 tests pass", MaxRuns: 5, Proposed: true}
	p, err := s.Start("net", "#c", "bob", "bob!u@h", goal, false, lim)
	if err != nil || p.Status != TaskProposed || p.MaxRuns != 5 || p.Criteria != "5 tests pass" {
		t.Fatalf("proposal = %+v, %v", p, err)
	}
	if _, ok := s.Claim("net"); ok {
		t.Error("a proposal ran before it was accepted")
	}
	if _, err := s.Start("net", "#c", "bob", "bob!u@h", job("x"), false, lim); !errors.Is(err, ErrTaskQuota) {
		t.Error("a proposal did not count toward the active limit")
	}
	if !s.Accept("net", p.ID) || s.Accept("net", p.ID) {
		t.Error("accept should work exactly once")
	}
	if got, ok := s.Claim("net"); !ok || got.ID != p.ID {
		t.Errorf("accepted goal not claimed: %+v %v", got, ok)
	}

	stale, _ := s.Start("net", "#c", "admin", "a!u@h", goal, true, lim)
	if n := s.ExpireProposals("net", -time.Second); n != 1 {
		t.Errorf("expired %d proposals, want 1", n)
	}
	if got, _ := s.Get("net", stale.ID); got.Status != TaskCancelled {
		t.Errorf("stale proposal = %s", got.Status)
	}
}

func TestTodoNotesAndFeedback(t *testing.T) {
	s := testTasks(t)
	task, _ := s.Start("net", "#c", "a", "a!u@h", TaskSpec{Kind: KindGoal, Objective: "o", MaxRuns: 3}, true, lim)
	if err := s.SetTodo(task.ID, []TodoItem{{Text: "write"}, {Text: "test", Done: true}}); err != nil {
		t.Fatal(err)
	}
	_ = s.AddNote(task.ID, "gcd(0,0) is 0 by convention")
	_ = s.AddNote(task.ID, "tests live in the paste")
	_ = s.SetFeedback(task.ID, "negative inputs untested", false)
	_ = s.SetFeedback(task.ID, "from bob: use abs()", true)

	got, _ := s.Get("net", task.ID)
	if len(got.Todo) != 2 || !got.Todo[1].Done || got.Todo[0].Text != "write" {
		t.Errorf("todo = %+v", got.Todo)
	}
	if got.Notes != "- gcd(0,0) is 0 by convention\n- tests live in the paste" {
		t.Errorf("notes = %q", got.Notes)
	}
	if got.Feedback != "negative inputs untested\nfrom bob: use abs()" {
		t.Errorf("feedback = %q", got.Feedback)
	}

	for i := 0; i < 200; i++ {
		_ = s.AddNote(task.ID, "a fairly long note that keeps being added to test the cap on notes")
	}
	if got, _ := s.Get("net", task.ID); len(got.Notes) > maxTaskNotes {
		t.Errorf("notes grew to %d chars", len(got.Notes))
	}
}

// A task table from before goals and schedules is brought up to date, keeping its rows.
func TestTaskTableMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`CREATE TABLE tasks (id INTEGER PRIMARY KEY AUTOINCREMENT, network TEXT NOT NULL,
		channel TEXT NOT NULL, owner TEXT NOT NULL, owner_mask TEXT NOT NULL, objective TEXT NOT NULL,
		status TEXT NOT NULL, result TEXT NOT NULL DEFAULT '', created INTEGER NOT NULL,
		started INTEGER NOT NULL DEFAULT 0, finished INTEGER NOT NULL DEFAULT 0);
		INSERT INTO tasks(network, channel, owner, owner_mask, objective, status, created)
		VALUES('net', '#c', 'a', 'a!u@h', 'old job', 'queued', 1);`); err != nil {
		t.Fatal(err)
	}
	old.Close()

	db, err := OpenContextDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err := NewTaskStore(db)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := s.Claim("net")
	if !ok || got.Objective != "old job" || got.Kind != KindTask || got.MaxRuns != 1 {
		t.Errorf("old row after migration = %+v %v", got, ok)
	}
}
