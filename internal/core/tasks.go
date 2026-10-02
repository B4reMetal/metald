// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Kinds of background work. A task runs once; a goal runs rounds until a reviewer passes it or its
// budget is spent; a schedule runs at a set time, and again every Interval if it has one.
const (
	KindTask     = "task"
	KindGoal     = "goal"
	KindSchedule = "schedule"
)

// Statuses. A proposed goal waits for its owner to accept it before it can run.
const (
	TaskProposed  = "proposed"
	TaskQueued    = "queued"
	TaskRunning   = "running"
	TaskDone      = "done"
	TaskFailed    = "failed"
	TaskCancelled = "cancelled"
)

const (
	maxTaskObjective = 2000
	maxTaskCriteria  = 1000
	maxTaskResult    = 8000
	maxTaskNotes     = 3000
	maxTaskFeedback  = 1000
	maxTodoItems     = 20
	maxTodoItem      = 200
	// MinScheduleInterval keeps a recurring schedule from hogging the model.
	MinScheduleInterval = 15 * time.Minute
)

// TodoItem is one line of a task's checklist.
type TodoItem struct {
	Text string `json:"text"`
	Done bool   `json:"done"`
}

// Task is one piece of background work.
type Task struct {
	ID        int64
	Kind      string
	Network   string
	Channel   string
	Owner     string // nick that asked
	OwnerMask string // their full hostmask when they asked, so admin checks still hold
	Objective string
	Criteria  string // for a goal: what counts as done
	Status    string
	Result    string
	Feedback  string // for a goal: the reviewer's last word, and any nudge from its owner
	Notes     string // what the work wrote down for later rounds
	Todo      []TodoItem
	Runs      int
	MaxRuns   int
	Spent     time.Duration // time the runs have taken so far
	Interval  time.Duration // for a recurring schedule
	Created   time.Time
	Started   time.Time
	Finished  time.Time
	NextRun   time.Time
	Deadline  time.Time
}

// TaskSpec is what someone asks for; Start turns it into a Task.
type TaskSpec struct {
	Kind      string
	Objective string
	Criteria  string
	MaxRuns   int
	NextRun   time.Time
	Interval  time.Duration
	Deadline  time.Time
	Proposed  bool // a goal the model suggested; its owner must accept it
}

// TaskLimits caps what one non-admin nick may start.
type TaskLimits struct {
	MaxActive  int // proposed, queued or running at once
	DailyLimit int // started in the last 24 hours; 0 means non-admins may not start any
}

// ErrTaskQuota is returned when a request is refused; its message is safe to show.
var ErrTaskQuota = errors.New("task quota")

// TaskStore keeps background work in the context database.
type TaskStore struct {
	db     *ContextDB
	paused sync.Map // network -> struct{}
}

var (
	globalTasks     *TaskStore
	globalTasksOnce sync.Once
	globalTasksErr  error
)

// Tasks returns the process-wide task store.
func Tasks() (*TaskStore, error) {
	globalTasksOnce.Do(func() {
		db, err := Context()
		if err != nil {
			globalTasksErr = err
			return
		}
		globalTasks, globalTasksErr = NewTaskStore(db)
	})
	return globalTasks, globalTasksErr
}

// NewTaskStore creates the task table, or brings an older one up to date.
func NewTaskStore(db *ContextDB) (*TaskStore, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if _, err := db.db.Exec(`
		CREATE TABLE IF NOT EXISTS tasks (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			network    TEXT NOT NULL,
			channel    TEXT NOT NULL,
			owner      TEXT NOT NULL,
			owner_mask TEXT NOT NULL,
			objective  TEXT NOT NULL,
			status     TEXT NOT NULL,
			result     TEXT NOT NULL DEFAULT '',
			created    INTEGER NOT NULL,
			started    INTEGER NOT NULL DEFAULT 0,
			finished   INTEGER NOT NULL DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS idx_tasks_net_status ON tasks(network, status);
	`); err != nil {
		return nil, fmt.Errorf("create task schema: %w", err)
	}
	for _, col := range []struct{ name, def string }{
		{"kind", "TEXT NOT NULL DEFAULT 'task'"},
		{"criteria", "TEXT NOT NULL DEFAULT ''"},
		{"feedback", "TEXT NOT NULL DEFAULT ''"},
		{"notes", "TEXT NOT NULL DEFAULT ''"},
		{"todo", "TEXT NOT NULL DEFAULT ''"},
		{"runs", "INTEGER NOT NULL DEFAULT 0"},
		{"max_runs", "INTEGER NOT NULL DEFAULT 1"},
		{"spent_ms", "INTEGER NOT NULL DEFAULT 0"},
		{"interval_s", "INTEGER NOT NULL DEFAULT 0"},
		{"next_run", "INTEGER NOT NULL DEFAULT 0"},
		{"deadline", "INTEGER NOT NULL DEFAULT 0"},
	} {
		if !hasColumn(db.db, "tasks", col.name) {
			if _, err := db.db.Exec(`ALTER TABLE tasks ADD COLUMN ` + col.name + ` ` + col.def); err != nil {
				return nil, fmt.Errorf("add task column %s: %w", col.name, err)
			}
		}
	}
	return &TaskStore{db: db}, nil
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n]
	}
	return s
}

// Start records new work, enforcing limits unless the owner is an admin. A recurring schedule is
// always admin-only.
func (s *TaskStore) Start(network, channel, owner, ownerMask string, spec TaskSpec, admin bool, limits TaskLimits) (Task, error) {
	spec.Objective = clip(spec.Objective, maxTaskObjective)
	if spec.Objective == "" {
		return Task{}, errors.New("a task needs an objective")
	}
	if spec.Kind == "" {
		spec.Kind = KindTask
	}
	if spec.MaxRuns <= 0 {
		spec.MaxRuns = 1
	}
	if spec.Interval > 0 {
		if !admin {
			return Task{}, fmt.Errorf("%w: only admins can set up something that repeats", ErrTaskQuota)
		}
		if spec.Interval < MinScheduleInterval {
			return Task{}, fmt.Errorf("%w: it can repeat at most every %s", ErrTaskQuota, MinScheduleInterval)
		}
	}
	owner = strings.ToLower(owner)
	now := time.Now()
	if spec.NextRun.IsZero() {
		spec.NextRun = now
	}
	status := TaskQueued
	if spec.Proposed {
		status = TaskProposed
	}

	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	if !admin {
		if limits.DailyLimit <= 0 {
			return Task{}, fmt.Errorf("%w: background work is admin-only here", ErrTaskQuota)
		}
		var active, today int
		_ = s.db.db.QueryRow(`SELECT count(*) FROM tasks WHERE network = ? AND owner = ? AND status IN (?, ?, ?)`,
			network, owner, TaskProposed, TaskQueued, TaskRunning).Scan(&active)
		_ = s.db.db.QueryRow(`SELECT count(*) FROM tasks WHERE network = ? AND owner = ? AND created >= ?`,
			network, owner, now.Add(-24*time.Hour).Unix()).Scan(&today)
		if limits.MaxActive > 0 && active >= limits.MaxActive {
			return Task{}, fmt.Errorf("%w: %s already has %d thing(s) in progress", ErrTaskQuota, owner, active)
		}
		if today >= limits.DailyLimit {
			return Task{}, fmt.Errorf("%w: %s has used all %d for today", ErrTaskQuota, owner, limits.DailyLimit)
		}
	}
	res, err := s.db.db.Exec(`INSERT INTO tasks(network, channel, owner, owner_mask, objective, status, created,
		kind, criteria, max_runs, interval_s, next_run, deadline) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		network, channel, owner, ownerMask, spec.Objective, status, now.Unix(),
		spec.Kind, clip(spec.Criteria, maxTaskCriteria), spec.MaxRuns, int64(spec.Interval/time.Second),
		spec.NextRun.Unix(), unixOrZero(spec.Deadline))
	if err != nil {
		return Task{}, err
	}
	id, _ := res.LastInsertId()
	t, _ := s.getLocked(network, id)
	return t, nil
}

func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func timeOrZero(sec int64) time.Time {
	if sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}

const taskColumns = `id, kind, network, channel, owner, owner_mask, objective, criteria, status, result, feedback,
	notes, todo, runs, max_runs, spent_ms, interval_s, created, started, finished, next_run, deadline`

func scanTask(row interface{ Scan(...any) error }) (Task, error) {
	var t Task
	var todo string
	var spentMs, intervalS, created, started, finished, next, deadline int64
	err := row.Scan(&t.ID, &t.Kind, &t.Network, &t.Channel, &t.Owner, &t.OwnerMask, &t.Objective, &t.Criteria,
		&t.Status, &t.Result, &t.Feedback, &t.Notes, &todo, &t.Runs, &t.MaxRuns, &spentMs, &intervalS,
		&created, &started, &finished, &next, &deadline)
	if todo != "" {
		_ = json.Unmarshal([]byte(todo), &t.Todo)
	}
	t.Spent = time.Duration(spentMs) * time.Millisecond
	t.Interval = time.Duration(intervalS) * time.Second
	t.Created = time.Unix(created, 0)
	t.Started, t.Finished = timeOrZero(started), timeOrZero(finished)
	t.NextRun, t.Deadline = timeOrZero(next), timeOrZero(deadline)
	return t, err
}

func (s *TaskStore) getLocked(network string, id int64) (Task, bool) {
	t, err := scanTask(s.db.db.QueryRow(`SELECT `+taskColumns+` FROM tasks WHERE network = ? AND id = ?`, network, id))
	return t, err == nil
}

// Get returns one task on a network.
func (s *TaskStore) Get(network string, id int64) (Task, bool) {
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	return s.getLocked(network, id)
}

// Claim marks the next due task on a network as running and returns it.
func (s *TaskStore) Claim(network string) (Task, bool) {
	if s.Paused(network) {
		return Task{}, false
	}
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	now := time.Now()
	t, err := scanTask(s.db.db.QueryRow(`SELECT `+taskColumns+` FROM tasks
		WHERE network = ? AND status = ? AND next_run <= ? ORDER BY next_run, id LIMIT 1`,
		network, TaskQueued, now.Unix()))
	if err != nil {
		return Task{}, false
	}
	if t.Started.IsZero() {
		t.Started = now
	}
	if _, err := s.db.db.Exec(`UPDATE tasks SET status = ?, started = ? WHERE id = ? AND status = ?`,
		TaskRunning, t.Started.Unix(), t.ID, TaskQueued); err != nil {
		return Task{}, false
	}
	t.Status = TaskRunning
	return t, true
}

// Finish records the end of a task's last run. A cancelled task stays cancelled.
func (s *TaskStore) Finish(id int64, status, result string, ran time.Duration) {
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	_, _ = s.db.db.Exec(`UPDATE tasks SET status = ?, result = ?, finished = ?, runs = runs + 1,
		spent_ms = spent_ms + ? WHERE id = ? AND status != ?`,
		status, clip(result, maxTaskResult), time.Now().Unix(), ran.Milliseconds(), id, TaskCancelled)
}

// Reschedule queues a task's next run: a goal's next round, or a schedule's next repeat.
func (s *TaskStore) Reschedule(id int64, next time.Time, result string, ran time.Duration) {
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	_, _ = s.db.db.Exec(`UPDATE tasks SET status = ?, result = ?, next_run = ?, runs = runs + 1,
		spent_ms = spent_ms + ? WHERE id = ? AND status != ?`,
		TaskQueued, clip(result, maxTaskResult), next.Unix(), ran.Milliseconds(), id, TaskCancelled)
}

// Cancel stops a proposed, queued or running task and reports whether it did.
func (s *TaskStore) Cancel(network string, id int64) bool {
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	res, err := s.db.db.Exec(`UPDATE tasks SET status = ?, finished = ? WHERE network = ? AND id = ? AND status IN (?, ?, ?)`,
		TaskCancelled, time.Now().Unix(), network, id, TaskProposed, TaskQueued, TaskRunning)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// Accept queues a proposed goal and reports whether there was one.
func (s *TaskStore) Accept(network string, id int64) bool {
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	res, err := s.db.db.Exec(`UPDATE tasks SET status = ?, next_run = ? WHERE network = ? AND id = ? AND status = ?`,
		TaskQueued, time.Now().Unix(), network, id, TaskProposed)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// ExpireProposals cancels proposals nobody accepted within ttl, and returns how many.
func (s *TaskStore) ExpireProposals(network string, ttl time.Duration) int64 {
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	res, err := s.db.db.Exec(`UPDATE tasks SET status = ?, finished = ? WHERE network = ? AND status = ? AND created < ?`,
		TaskCancelled, time.Now().Unix(), network, TaskProposed, time.Now().Add(-ttl).Unix())
	if err != nil {
		return 0
	}
	n, _ := res.RowsAffected()
	return n
}

// SetTodo replaces a task's checklist.
func (s *TaskStore) SetTodo(id int64, items []TodoItem) error {
	if len(items) > maxTodoItems {
		items = items[:maxTodoItems]
	}
	for i := range items {
		items[i].Text = clip(items[i].Text, maxTodoItem)
	}
	raw, _ := json.Marshal(items)
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	_, err := s.db.db.Exec(`UPDATE tasks SET todo = ? WHERE id = ?`, string(raw), id)
	return err
}

// AddNote appends to a task's notes, keeping the newest when they grow past their limit.
func (s *TaskStore) AddNote(id int64, note string) error {
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	var notes string
	if err := s.db.db.QueryRow(`SELECT notes FROM tasks WHERE id = ?`, id).Scan(&notes); err != nil {
		return err
	}
	notes = strings.TrimSpace(notes + "\n- " + clip(note, 500))
	if len(notes) > maxTaskNotes {
		notes = notes[len(notes)-maxTaskNotes:]
	}
	_, err := s.db.db.Exec(`UPDATE tasks SET notes = ? WHERE id = ?`, notes, id)
	return err
}

// SetFeedback replaces what the next round of a goal is told, or adds to it when add is set.
func (s *TaskStore) SetFeedback(id int64, feedback string, add bool) error {
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	if add {
		var cur string
		_ = s.db.db.QueryRow(`SELECT feedback FROM tasks WHERE id = ?`, id).Scan(&cur)
		feedback = strings.TrimSpace(cur + "\n" + feedback)
	}
	_, err := s.db.db.Exec(`UPDATE tasks SET feedback = ? WHERE id = ?`, clip(feedback, maxTaskFeedback), id)
	return err
}

// List returns a network's unfinished work, then its most recent finished tasks.
func (s *TaskStore) List(network string, recent int) ([]Task, error) {
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	rows, err := s.db.db.Query(`SELECT `+taskColumns+` FROM tasks WHERE network = ? AND status IN (?, ?, ?)
		UNION ALL SELECT * FROM (SELECT `+taskColumns+` FROM tasks WHERE network = ? AND status NOT IN (?, ?, ?)
		ORDER BY id DESC LIMIT ?)`,
		network, TaskProposed, TaskQueued, TaskRunning, network, TaskProposed, TaskQueued, TaskRunning, recent)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Requeue puts tasks a restart interrupted back in the queue, and returns how many.
func (s *TaskStore) Requeue(network string) int64 {
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	res, err := s.db.db.Exec(`UPDATE tasks SET status = ? WHERE network = ? AND status = ?`, TaskQueued, network, TaskRunning)
	if err != nil {
		return 0
	}
	n, _ := res.RowsAffected()
	return n
}

// Pause stops a network's runner from starting work; whatever is running finishes.
func (s *TaskStore) Pause(network string)  { s.paused.Store(network, struct{}{}) }
func (s *TaskStore) Resume(network string) { s.paused.Delete(network) }

// Paused reports whether a network's work is paused.
func (s *TaskStore) Paused(network string) bool {
	_, ok := s.paused.Load(network)
	return ok
}
