// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package admin

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"B4reMetal/metald/internal/core"
)

type fakeTasks struct {
	tasks  []core.Task
	paused map[string]bool
}

func (f *fakeTasks) List(network string, _ int) ([]core.Task, error) {
	var out []core.Task
	for _, t := range f.tasks {
		if t.Network == network {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fakeTasks) Cancel(network string, id int64) bool {
	for i, t := range f.tasks {
		if t.Network == network && t.ID == id && (t.Status == core.TaskQueued || t.Status == core.TaskRunning) {
			f.tasks[i].Status = core.TaskCancelled
			return true
		}
	}
	return false
}
func (f *fakeTasks) Pause(n string)       { f.paused[n] = true }
func (f *fakeTasks) Resume(n string)      { delete(f.paused, n) }
func (f *fakeTasks) Paused(n string) bool { return f.paused[n] }

type fakeReminders struct{ list []core.Reminder }

func (f *fakeReminders) List(network string) []core.Reminder {
	var out []core.Reminder
	for _, r := range f.list {
		if r.Network == network {
			out = append(out, r)
		}
	}
	return out
}

func (f *fakeReminders) Cancel(network, id string) bool {
	for i, r := range f.list {
		if r.Network == network && r.ID == id {
			f.list = append(f.list[:i], f.list[i+1:]...)
			return true
		}
	}
	return false
}

type fakeWork struct{ stopped []int64 }

func (f *fakeWork) Inflight() []core.Inflight {
	return []core.Inflight{{Key: "net/#chat", Operation: "addressed", Source: "alice", Since: time.Now(), Running: true}}
}
func (f *fakeWork) Concurrency() int   { return 3 }
func (f *fakeWork) StopRound(id int64) { f.stopped = append(f.stopped, id) }

type rig struct {
	tasks    *fakeTasks
	rems     *fakeReminders
	work     *fakeWork
	logs     *core.LiveHub
	thinking *core.LiveHub
	srv      *Server
	ts       *httptest.Server
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{
		tasks: &fakeTasks{paused: map[string]bool{}, tasks: []core.Task{
			{ID: 7, Network: "net", Kind: core.KindTask, Status: core.TaskRunning, Objective: "write a haiku"},
			{ID: 8, Network: "net", Kind: core.KindSchedule, Status: core.TaskDone, Objective: "old"},
		}},
		rems: &fakeReminders{list: []core.Reminder{{ID: "abc", Network: "net", Nick: "bob", Text: "stretch"}}},
		work: &fakeWork{}, logs: core.NewLiveHub(10), thinking: core.NewLiveHub(10),
	}
	r.srv = New(Config{Token: "s3cret", Version: "v-test", Networks: []string{"net"}, Started: time.Now()},
		r.tasks, r.rems, r.work, Feeds{Logs: r.logs, Thinking: r.thinking}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.ts = httptest.NewServer(r.srv.Handler())
	t.Cleanup(r.ts.Close)
	return r
}

func (r *rig) call(t *testing.T, method, path, token, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, r.ts.URL+path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func asJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestEveryAPICallNeedsTheToken(t *testing.T) {
	r := newRig(t)
	calls := [][2]string{{"GET", "/api/v1/status"}, {"GET", "/api/v1/tasks"}, {"GET", "/api/v1/reminders"},
		{"GET", "/api/v1/gpu"}, {"GET", "/api/v1/radio"}, {"POST", "/api/v1/tasks/net/7/cancel"},
		{"PUT", "/api/v1/networks/net/paused"}, {"DELETE", "/api/v1/reminders/net/abc"}, {"GET", "/api/v1/nope"},
		{"GET", "/api/v1/logs/stream"}, {"GET", "/api/v1/thinking/stream"}, {"POST", "/api/v1/gpu/x/cancel"}}
	for _, c := range calls {
		for _, token := range []string{"", "wrong", "s3cre", "s3cretx"} {
			if code, _ := r.call(t, c[0], c[1], token, `{"paused":true}`); code != http.StatusUnauthorized {
				t.Errorf("%s %s with %q = %d, want 401", c[0], c[1], token, code)
			}
		}
	}
	if r.tasks.tasks[0].Status != core.TaskRunning || len(r.rems.list) != 1 || r.tasks.paused["net"] {
		t.Fatal("a refused call changed something")
	}
}

func TestNoTokenNoServer(t *testing.T) {
	s := New(Config{Networks: []string{"net"}}, &fakeTasks{}, &fakeReminders{}, &fakeWork{}, Feeds{}, slog.Default())
	if err := s.Run(context.Background(), "127.0.0.1:0"); err == nil {
		t.Fatal("Run started without a token")
	}
}

func TestStatus(t *testing.T) {
	r := newRig(t)
	code, out := r.call(t, "GET", "/api/v1/status", "s3cret", "")
	if code != http.StatusOK || out["version"] != "v-test" || out["concurrency"] != 3.0 {
		t.Fatalf("status = %d %v", code, out)
	}
	if !strings.Contains(asJSON(out["inflight"]), `"source":"alice"`) {
		t.Errorf("inflight = %v", out["inflight"])
	}
}

func TestCancellingATaskStopsItsRound(t *testing.T) {
	r := newRig(t)
	_, out := r.call(t, "GET", "/api/v1/tasks", "s3cret", "")
	if !strings.Contains(asJSON(out), `"objective":"write a haiku"`) || !strings.Contains(asJSON(out), `"active":true`) {
		t.Fatalf("tasks = %v", out)
	}
	if code, _ := r.call(t, "POST", "/api/v1/tasks/net/7/cancel", "s3cret", ""); code != http.StatusOK {
		t.Fatalf("cancel = %d", code)
	}
	if r.tasks.tasks[0].Status != core.TaskCancelled || asJSON(r.work.stopped) != "[7]" {
		t.Errorf("after cancel: status %s, stopped %v", r.tasks.tasks[0].Status, r.work.stopped)
	}
	for path, want := range map[string]int{
		"/api/v1/tasks/net/7/cancel":   http.StatusConflict, // already cancelled
		"/api/v1/tasks/net/8/cancel":   http.StatusConflict, // finished
		"/api/v1/tasks/other/7/cancel": http.StatusNotFound,
		"/api/v1/tasks/net/x/cancel":   http.StatusBadRequest,
	} {
		if code, _ := r.call(t, "POST", path, "s3cret", ""); code != want {
			t.Errorf("POST %s = %d, want %d", path, code, want)
		}
	}
}

func TestPauseAndResume(t *testing.T) {
	r := newRig(t)
	if code, _ := r.call(t, "PUT", "/api/v1/networks/net/paused", "s3cret", `{"paused":true}`); code != http.StatusOK || !r.tasks.paused["net"] {
		t.Fatalf("pause = %d", code)
	}
	if code, _ := r.call(t, "PUT", "/api/v1/networks/net/paused", "s3cret", `{"paused":false}`); code != http.StatusOK || r.tasks.paused["net"] {
		t.Fatalf("resume = %d", code)
	}
	if code, _ := r.call(t, "PUT", "/api/v1/networks/net/paused", "s3cret", `{}`); code != http.StatusBadRequest {
		t.Errorf("no paused field = %d", code)
	}
}

func TestCancelAReminder(t *testing.T) {
	r := newRig(t)
	if _, out := r.call(t, "GET", "/api/v1/reminders", "s3cret", ""); !strings.Contains(asJSON(out), `"text":"stretch"`) {
		t.Fatalf("reminders = %v", out)
	}
	if code, _ := r.call(t, "DELETE", "/api/v1/reminders/net/abc", "s3cret", ""); code != http.StatusOK || len(r.rems.list) != 0 {
		t.Fatalf("delete = %d, left %v", code, r.rems.list)
	}
	if code, _ := r.call(t, "DELETE", "/api/v1/reminders/net/abc", "s3cret", ""); code != http.StatusNotFound {
		t.Errorf("deleting twice = %d", code)
	}
}

func fakeComfy(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var posts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			b, _ := io.ReadAll(r.Body)
			posts = append(posts, r.URL.Path+" "+string(b))
			return
		}
		_, _ = w.Write([]byte(`{"queue_running": [[1, "run-1", {"3": {"class_type": "EmptyAceStepLatentAudio", "inputs": {"tags": "  sea   shanty, accordion "}}}, {"metald": {"tool": "musicgen"}}, []]],
			"queue_pending": [[5, "wait-2", {"9": {"class_type": "SaveImage", "inputs": {}}, "6": {"class_type": "CLIPTextEncode", "inputs": {"text": "a cat"}}}, {}, []],
			                  [3, "wait-1", {"1": {"class_type": "ChatterboxTTS", "inputs": {"text": "hello"}}}, {"metald": {"tool": "radio"}}, []]]}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &posts
}

func TestGPUQueueDetails(t *testing.T) {
	comfy, _ := fakeComfy(t)
	r := newRig(t)
	r.srv.cfg.ComfyURL = comfy.URL
	_, g := r.call(t, "GET", "/api/v1/gpu", "s3cret", "")
	var jobs []gpuJob
	_ = json.Unmarshal([]byte(asJSON(g["jobs"])), &jobs)
	if len(jobs) != 3 {
		t.Fatalf("jobs = %v", g)
	}
	want := []gpuJob{
		{ID: "run-1", State: "running", Position: 0, Kind: "song", Tool: "musicgen", Summary: "sea shanty, accordion"},
		{ID: "wait-1", State: "waiting", Position: 1, Kind: "speech", Tool: "radio", Summary: "hello"},
		{ID: "wait-2", State: "waiting", Position: 2, Kind: "image", Summary: "a cat"},
	}
	for i, w := range want {
		got := jobs[i]
		got.Since = 0
		if got != w {
			t.Errorf("job %d = %+v, want %+v", i, got, w)
		}
		if jobs[i].Since == 0 {
			t.Errorf("job %d has no first-seen time", i)
		}
	}
	r.srv.cfg.ComfyURL = "http://127.0.0.1:1"
	if _, g := r.call(t, "GET", "/api/v1/gpu", "s3cret", ""); g["reachable"] != false || g["configured"] != true {
		t.Errorf("unreachable gpu = %v", g)
	}
}

func TestCancelAGPUJob(t *testing.T) {
	comfy, posts := fakeComfy(t)
	r := newRig(t)
	r.srv.cfg.ComfyURL = comfy.URL
	for id, want := range map[string]int{"wait-1": 200, "run-1": 200, "gone": 404} {
		if code, _ := r.call(t, "POST", "/api/v1/gpu/"+id+"/cancel", "s3cret", ""); code != want {
			t.Errorf("cancel %s = %d, want %d", id, code, want)
		}
	}
	got := strings.Join(*posts, "\n")
	if !strings.Contains(got, `/queue {"delete":["wait-1"]}`) || !strings.Contains(got, `/interrupt {"prompt_id":"run-1"}`) || len(*posts) != 2 {
		t.Errorf("posted to comfy: %s", got)
	}
}

func TestRadio(t *testing.T) {
	radio := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/now" {
			_, _ = w.Write([]byte(`{"title": "blue hair", "listeners": 3}`))
			return
		}
		_, _ = w.Write([]byte(`{"queue": ["next one"]}`))
	}))
	defer radio.Close()
	r := newRig(t)
	r.srv.cfg.RadioURL = radio.URL
	if _, rd := r.call(t, "GET", "/api/v1/radio", "s3cret", ""); rd["title"] != "blue hair" || asJSON(rd["queue"]) != `["next one"]` {
		t.Errorf("radio = %v", rd)
	}
}

func TestTheAppIsServedWithoutTheToken(t *testing.T) {
	r := newRig(t)
	for _, path := range []string{"/", "/tasks/deep/link"} {
		resp, err := http.Get(r.ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `<div id="app">`) {
			t.Errorf("%s = %d", path, resp.StatusCode)
		}
	}
}

func TestSignInThroughTheAuthProxy(t *testing.T) {
	r := newRig(t)
	local := netip.MustParsePrefix("127.0.0.1/32") // where the test client connects from
	r.srv.cfg.UserHeader = "X-Authentik-Username"
	asUser := func(user string) int {
		req, _ := http.NewRequest("GET", r.ts.URL+"/api/v1/whoami", nil)
		req.Header.Set("X-Authentik-Username", user)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	r.srv.cfg.TrustedProxies, r.srv.cfg.Users = []netip.Prefix{local}, []string{"Alice"}
	if code := asUser("alice"); code != http.StatusOK {
		t.Errorf("listed user via the trusted proxy = %d", code)
	}
	if code := asUser("mallory"); code != http.StatusUnauthorized {
		t.Errorf("unlisted user = %d", code)
	}
	if code := asUser(""); code != http.StatusUnauthorized {
		t.Errorf("no user header = %d", code)
	}

	r.srv.cfg.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("10.9.9.9/32")}
	if code := asUser("alice"); code != http.StatusUnauthorized {
		t.Errorf("header from an untrusted address = %d", code)
	}

	r.srv.cfg.TrustedProxies, r.srv.cfg.Users = []netip.Prefix{local}, nil
	if code := asUser("alice"); code != http.StatusUnauthorized {
		t.Errorf("no users configured = %d", code)
	}

	r.srv.cfg.Users = []string{"alice"}
	req, _ := http.NewRequest("GET", r.ts.URL+"/api/v1/whoami", nil)
	req.Header.Set("X-Authentik-Username", "alice")
	resp, _ := http.DefaultClient.Do(req)
	var who map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&who)
	resp.Body.Close()
	if who["user"] != "alice" || who["via"] != "proxy" {
		t.Errorf("whoami = %v", who)
	}
	if _, out := r.call(t, "GET", "/api/v1/whoami", "s3cret", ""); out["via"] != "token" {
		t.Errorf("whoami by token = %v", out)
	}
}

func TestLogsStreamTheBacklogThenLiveEvents(t *testing.T) {
	r := newRig(t)
	r.logs.Publish(core.LiveEvent{Level: "INFO", Message: "before"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", r.ts.URL+"/api/v1/logs/stream", nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	lines := bufio.NewScanner(resp.Body)
	next := func() core.LiveEvent {
		for lines.Scan() {
			if data, ok := strings.CutPrefix(lines.Text(), "data: "); ok {
				var e core.LiveEvent
				_ = json.Unmarshal([]byte(data), &e)
				return e
			}
		}
		t.Fatal("stream ended")
		return core.LiveEvent{}
	}
	if e := next(); e.Message != "before" {
		t.Fatalf("backlog = %+v", e)
	}
	r.logs.Publish(core.LiveEvent{Level: "WARN", Message: "after"})
	if e := next(); e.Message != "after" || e.Level != "WARN" {
		t.Fatalf("live = %+v", e)
	}
}
