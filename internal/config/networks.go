// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// networkYAML is one entry of the optional "networks:" list in config.yml.
type networkYAML struct {
	Name        *string `yaml:"name"`
	Nick        *string `yaml:"nick"`
	Server      *string `yaml:"server"`
	Port        *int    `yaml:"port"`
	Channel     *string `yaml:"channel"`
	ChannelKey  *string `yaml:"channelkey"`
	TLS         *bool   `yaml:"tls"`
	TLSInsecure *bool   `yaml:"tlsinsecure"`
	SASLNick    *string `yaml:"saslnick"`
	SASLPass    *string `yaml:"saslpass"`
	ServerPass  *string `yaml:"serverpass"`
	// ResponsePrefix overrides the shared Bot.ResponsePrefix here only.
	ResponsePrefix *string `yaml:"responseprefix"`
}

// NetworkName returns the label used for logs and, more importantly, as the prefix on lock and
// session keys.
func networkName(n networkYAML, fallback string) string {
	if n.Name != nil && strings.TrimSpace(*n.Name) != "" {
		return strings.TrimSpace(*n.Name)
	}
	if n.Server != nil && strings.TrimSpace(*n.Server) != "" {
		return strings.TrimSpace(*n.Server)
	}
	return fallback
}

// parseNetworks reads the "networks:" list out of the config file, layering each entry over the
// base server config.
func parseNetworks(path string, base *ServerConfig) ([]*ServerConfig, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil // absence of a readable config is handled elsewhere
	}

	var doc struct {
		Networks []networkYAML `yaml:"networks"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing networks: %w", err)
	}
	if len(doc.Networks) == 0 {
		return nil, nil
	}

	seen := map[string]bool{}
	out := make([]*ServerConfig, 0, len(doc.Networks))
	for i, n := range doc.Networks {
		s := *base // copy: inherit every top-level setting
		s.Name = networkName(n, fmt.Sprintf("network%d", i+1))

		if n.Nick != nil {
			s.Nick = *n.Nick
		}
		if n.Server != nil {
			s.Server = *n.Server
		}
		if n.Port != nil {
			s.Port = *n.Port
		}
		if n.Channel != nil {
			s.Channel = *n.Channel
		}
		if n.ChannelKey != nil {
			s.ChannelKey = *n.ChannelKey
		}
		if n.TLS != nil {
			s.SSL = *n.TLS
		}
		if n.TLSInsecure != nil {
			s.TLSInsecure = *n.TLSInsecure
		}
		if n.SASLNick != nil {
			s.SASLNick = *n.SASLNick
		}
		if n.SASLPass != nil {
			s.SASLPass = *n.SASLPass
		}
		if n.ServerPass != nil {
			s.ServerPass = *n.ServerPass
		}
		if n.ResponsePrefix != nil {
			v := *n.ResponsePrefix
			s.ResponsePrefix = &v
		}

		if s.Server == "" {
			return nil, fmt.Errorf("network %q has no server", s.Name)
		}
		// Duplicate names would collide in the lock and session keyspace, silently merging two networks'
		// conversations.
		if seen[s.Name] {
			return nil, fmt.Errorf("duplicate network name %q", s.Name)
		}
		seen[s.Name] = true

		out = append(out, &s)
	}
	return out, nil
}

// ForNetwork returns a Configuration that is this one with a different server.
func (c *Configuration) ForNetwork(s *ServerConfig) *Configuration {
	clone := *c
	clone.Server = s
	return &clone
}
