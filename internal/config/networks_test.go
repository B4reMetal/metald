// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCfg(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func base() *ServerConfig {
	return &ServerConfig{
		Nick: "BareMetal", Server: "irc.example.org", Port: 6697,
		Channel: "#chat", SSL: true, ServerPass: "topsecret",
	}
}

// A config with no networks: list must behave exactly as before. This is the
// upgrade path for every existing deployment.
func TestNoNetworksListKeepsSingleServer(t *testing.T) {
	p := writeCfg(t, "nick: BareMetal\nserver: irc.example.org\n")
	nets, err := parseNetworks(p, base())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nets != nil {
		t.Errorf("expected nil (single-server path), got %d networks", len(nets))
	}
}

// Unset fields inherit from the top-level config, so a second network only
// has to name what differs.
func TestNetworksInheritTopLevelSettings(t *testing.T) {
	p := writeCfg(t, `
networks:
  - name: live
    server: irc.examplenet.org
  - name: testbed
    server: 127.0.0.1
    port: 6667
    tls: false
    channel: '#test'
`)
	nets, err := parseNetworks(p, base())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nets) != 2 {
		t.Fatalf("expected 2 networks, got %d", len(nets))
	}

	live, test := nets[0], nets[1]
	// Inherited
	if live.Nick != "BareMetal" || live.Port != 6697 || !live.SSL || live.Channel != "#chat" {
		t.Errorf("live network did not inherit defaults: %+v", live)
	}
	if test.Nick != "BareMetal" {
		t.Errorf("testbed should inherit the nick, got %q", test.Nick)
	}
	// Overridden
	if test.Port != 6667 || test.SSL || test.Channel != "#test" {
		t.Errorf("testbed overrides not applied: %+v", test)
	}
	// Credentials inherit too, which is what makes a short testbed entry work
	if test.ServerPass != "topsecret" {
		t.Errorf("serverpass should inherit, got %q", test.ServerPass)
	}
}

// tls: false must override an inherited tls: true; only a pointer can tell absent from false.
func TestExplicitFalseOverridesInheritedTrue(t *testing.T) {
	p := writeCfg(t, "networks:\n  - name: plain\n    server: 127.0.0.1\n    tls: false\n")
	nets, _ := parseNetworks(p, base())
	if len(nets) != 1 {
		t.Fatalf("expected 1 network, got %d", len(nets))
	}
	if nets[0].SSL {
		t.Error("explicit 'tls: false' was ignored; it inherited true")
	}
}

// Duplicate names would collide in the lock and session keyspace, merging two
// networks' conversations. Refused at load time.
func TestDuplicateNetworkNamesRejected(t *testing.T) {
	p := writeCfg(t, `
networks:
  - name: dup
    server: a.example.org
  - name: dup
    server: b.example.org
`)
	if _, err := parseNetworks(p, base()); err == nil {
		t.Error("duplicate network names should be refused")
	}
}

// An unnamed network falls back to its hostname rather than to a shared
// blank, which would collide exactly like a duplicate name.
func TestUnnamedNetworksGetDistinctNames(t *testing.T) {
	p := writeCfg(t, `
networks:
  - server: a.example.org
  - server: b.example.org
`)
	nets, err := parseNetworks(p, base())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nets[0].Name == nets[1].Name {
		t.Errorf("unnamed networks collided on name %q", nets[0].Name)
	}
	if nets[0].Name != "a.example.org" {
		t.Errorf("expected hostname fallback, got %q", nets[0].Name)
	}
}

func TestNetworkWithoutServerRejected(t *testing.T) {
	p := writeCfg(t, "networks:\n  - name: broken\n    channel: '#x'\n")
	b := base()
	b.Server = ""
	if _, err := parseNetworks(p, b); err == nil {
		t.Error("a network with no server should be refused")
	}
}

// ForNetwork must share the process-wide sub-configs and swap only Server.
// If Bot were copied, a "+set" on one network would not reach the others.
func TestForNetworkSharesEverythingButServer(t *testing.T) {
	cfg := &Configuration{
		Server: base(), Bot: &BotConfig{Trigger: "metalai"},
		Model:   &ModelConfig{Model: "openai/chat"},
		Session: &SessionConfig{}, API: &APIConfig{},
	}
	other := &ServerConfig{Name: "testbed", Server: "127.0.0.1"}
	clone := cfg.ForNetwork(other)

	if clone.Server != other {
		t.Error("ForNetwork did not swap the server")
	}
	if clone.Bot != cfg.Bot || clone.Model != cfg.Model || clone.API != cfg.API {
		t.Error("sub-configs must be shared, not copied")
	}
	// Mutating through one must be visible through the other.
	clone.Bot.Trigger = "changed"
	if cfg.Bot.Trigger != "changed" {
		t.Error("Bot config is not shared between networks")
	}
	if cfg.Server == other {
		t.Error("ForNetwork mutated the original configuration")
	}
}

// A network may silence the shared response prefix. Pointer semantics matter:
// "responseprefix: ”" must clear it, not read as absent and inherit "[metalai]".
func TestPerNetworkResponsePrefixOverride(t *testing.T) {
	p := writeCfg(t, `
networks:
  - name: live
    server: a.example.org
  - name: testbed
    server: 127.0.0.1
    nick: metalai
    responseprefix: ''
`)
	nets, err := parseNetworks(p, base())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if nets[0].ResponsePrefix != nil {
		t.Error("a network that says nothing about the prefix must inherit")
	}
	if nets[1].ResponsePrefix == nil {
		t.Fatal("explicit empty prefix was read as absent")
	}
	if *nets[1].ResponsePrefix != "" {
		t.Errorf("expected cleared prefix, got %q", *nets[1].ResponsePrefix)
	}
	if nets[1].Nick != "metalai" {
		t.Errorf("per-network nick not applied: %q", nets[1].Nick)
	}
}

// EffectiveResponsePrefix resolves override-then-shared.
func TestEffectiveResponsePrefix(t *testing.T) {
	bot := &BotConfig{ResponsePrefix: "[metalai]"}
	empty := ""

	inherit := &Configuration{Bot: bot, Server: &ServerConfig{Name: "live"}}
	if got := inherit.EffectiveResponsePrefix(); got != "[metalai]" {
		t.Errorf("should inherit the shared prefix, got %q", got)
	}

	silenced := &Configuration{Bot: bot, Server: &ServerConfig{Name: "testbed", ResponsePrefix: &empty}}
	if got := silenced.EffectiveResponsePrefix(); got != "" {
		t.Errorf("override should silence the prefix, got %q", got)
	}

	// The shared setting must be untouched by the override.
	if bot.ResponsePrefix != "[metalai]" {
		t.Error("the per-network override mutated the shared config")
	}
}
