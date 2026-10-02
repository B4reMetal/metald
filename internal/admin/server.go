// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

// Package admin serves the operator page: what the bot is doing, its queued and scheduled work (with
// cancel and pause), reminders, and the GPU and radio queues its tools use. The page itself is the
// Svelte app in web/admin, built into dist/ and embedded here.
package admin

import (
	"context"
	"crypto/subtle"
	"embed"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"time"
)

//go:embed all:dist
var dist embed.FS

// Config is what the server needs from the bot.
type Config struct {
	Token    string
	Version  string
	Networks []string
	Started  time.Time
	// Outside services the page reports on; empty ones are shown as not configured.
	ComfyURL, RadioURL string
	// Sign-in through an auth proxy (e.g. Traefik forward auth): a request from one of TrustedProxies
	// whose UserHeader names one of Users is let in without the token. Users empty: nobody is.
	TrustedProxies []netip.Prefix
	UserHeader     string
	Users          []string
}

// Server is the operator page for one bot process.
type Server struct {
	cfg       Config
	tasks     TaskService
	reminders ReminderService
	work      WorkService
	feeds     Feeds
	seen      firstSeen
	client    *http.Client
	log       *slog.Logger
}

// New builds a server over the given services; NewFromCore wires the bot's own.
func New(cfg Config, tasks TaskService, reminders ReminderService, work WorkService, feeds Feeds, log *slog.Logger) *Server {
	return &Server{cfg: cfg, tasks: tasks, reminders: reminders, work: work, feeds: feeds, log: log,
		client: &http.Client{Timeout: 5 * time.Second}}
}

// Run serves until ctx ends. It refuses to start without a token: the page can stop work, so it is
// never open, and the token is the way in when the auth proxy is down.
func (s *Server) Run(ctx context.Context, addr string) error {
	if s.cfg.Token == "" {
		return errors.New("admintoken is empty")
	}
	srv := &http.Server{Addr: addr, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
	}()
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Handler is the API under /api/v1 and the app everywhere else.
func (s *Server) Handler() http.Handler {
	api := http.NewServeMux()
	api.HandleFunc("GET /whoami", s.whoami)
	api.HandleFunc("GET /status", s.status)
	api.HandleFunc("GET /tasks", s.listTasks)
	api.HandleFunc("POST /tasks/{network}/{id}/cancel", s.cancelTask)
	api.HandleFunc("PUT /networks/{network}/paused", s.setPaused)
	api.HandleFunc("GET /reminders", s.listReminders)
	api.HandleFunc("DELETE /reminders/{network}/{id}", s.cancelReminder)
	api.HandleFunc("GET /gpu", s.gpu)
	api.HandleFunc("POST /gpu/{id}/cancel", s.cancelGPU)
	api.HandleFunc("GET /radio", s.radio)
	api.HandleFunc("GET /logs/stream", stream(s.feeds.Logs))
	api.HandleFunc("GET /thinking/stream", stream(s.feeds.Thinking))
	api.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { fail(w, http.StatusNotFound, "no such endpoint") })

	mux := http.NewServeMux()
	mux.Handle("/api/v1/", http.StripPrefix("/api/v1", s.authenticate(api)))
	mux.Handle("/", app())
	return s.logRequests(mux)
}

// authenticate lets in the token, or a user the auth proxy vouches for. The token travels in a header,
// never a cookie, so another site can't make a browser act with it.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		byToken := ok && s.cfg.Token != "" && subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.Token)) == 1
		if !byToken && s.proxyUser(r) == "" {
			fail(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// proxyUser is the user an auth proxy signed in, or "". The header is believed only from a trusted
// proxy's address, so someone reaching this server directly can't claim to be anyone.
func (s *Server) proxyUser(r *http.Request) string {
	if len(s.cfg.Users) == 0 || s.cfg.UserHeader == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return ""
	}
	addr, err := netip.ParseAddr(host)
	if err != nil || !slices.ContainsFunc(s.cfg.TrustedProxies, func(p netip.Prefix) bool { return p.Contains(addr.Unmap()) }) {
		return ""
	}
	user := strings.TrimSpace(r.Header.Get(s.cfg.UserHeader))
	if user == "" || !slices.ContainsFunc(s.cfg.Users, func(u string) bool { return strings.EqualFold(u, user) }) {
		return ""
	}
	return user
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) { r.status = code; r.ResponseWriter.WriteHeader(code) }

// Unwrap lets http.ResponseController reach the connection, so streams can flush through the recorder.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// logRequests records every change made through the page, and refused API calls; reads are not logged.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if strings.HasPrefix(r.URL.Path, "/api/") && (r.Method != http.MethodGet || rec.status == http.StatusUnauthorized) {
			s.log.Info("admin_request", "method", r.Method, "path", r.URL.Path, "status", rec.status,
				"remote", r.RemoteAddr, "user", s.proxyUser(r))
		}
	})
}

// app serves the built page, and index.html for any path that isn't a file, so the app's own routes
// survive a reload.
func app() http.Handler {
	files, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // embedded at build time; can't fail at run time
	}
	static := http.FileServerFS(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name != "" {
			if _, err := fs.Stat(files, name); err == nil {
				if strings.HasPrefix(name, "assets/") { // hashed names: safe to cache for good
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				static.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFileFS(w, r, files, "index.html")
	})
}
