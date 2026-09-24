// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package config

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

var envName = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// ToolEnv reads the "env:" section of config.yml: settings for tools, given to
// every tool as environment variables. Lists are joined with commas.
func ToolEnv(section any) (map[string]string, error) {
	if section == nil {
		return nil, nil
	}
	m, ok := section.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("env: must be a map of NAME: value")
	}
	out := make(map[string]string, len(m))
	for name, v := range m {
		if !envName.MatchString(name) {
			return nil, fmt.Errorf("env: %q is not a valid variable name (use UPPER_CASE)", name)
		}
		if strings.HasPrefix(name, "METALD_") {
			return nil, fmt.Errorf("env: %s is a bot setting; set it as a top-level key instead", name)
		}
		s, err := envValue(v)
		if err != nil {
			return nil, fmt.Errorf("env: %s: %w", name, err)
		}
		out[name] = s
	}
	return out, nil
}

func envValue(v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "", nil
	case string:
		return x, nil
	case bool, int, int64, uint64, float64:
		return fmt.Sprint(x), nil
	case []any:
		parts := make([]string, 0, len(x))
		for _, e := range x {
			s, err := envValue(e)
			if err != nil {
				return "", err
			}
			if _, isList := e.([]any); isList {
				return "", fmt.Errorf("nested lists are not supported")
			}
			parts = append(parts, s)
		}
		return strings.Join(parts, ","), nil
	default:
		return "", fmt.Errorf("value must be text, a number, a boolean or a list")
	}
}

// ExportToolEnv sets each variable unless the real environment already has
// it, so a secret can still be injected with docker run -e. It returns the
// names it set and the names left to the environment.
func ExportToolEnv(vars map[string]string) (set, fromEnv []string) {
	for name, value := range vars {
		if os.Getenv(name) != "" {
			fromEnv = append(fromEnv, name)
			continue
		}
		os.Setenv(name, value)
		set = append(set, name)
	}
	sort.Strings(set)
	sort.Strings(fromEnv)
	return set, fromEnv
}
