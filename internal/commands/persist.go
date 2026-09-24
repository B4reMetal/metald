// Copyright (C) 2026 BareMetal
// Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
// SPDX-License-Identifier: GPL-3.0-only

package commands

import (
	"encoding/json"
	"os"
	"sync"

	"B4reMetal/metald/internal/config"
	"B4reMetal/metald/internal/core"
)

// OverridesPath is where runtime configuration changes are persisted.
var OverridesPath = "config-overrides.json"

// runtimeOverrides is the on-disk shape. Only fields actually changed at
// runtime appear; absent entries mean "defer to config.yml".
type runtimeOverrides struct {
	// Set holds the RAW values passed to "+set <key> <value>", not values read back from the getters.
	Set map[string]string `json:"set,omitempty"`

	// Pointers, not plain slices: these have to distinguish "never overridden - defer to config.yml"
	// from "overridden to an empty list".
	Admins      *[]string `json:"admins,omitempty"`
	AdminTools  *[]string `json:"admintools,omitempty"`
	ScreenNicks *[]string `json:"screennicks,omitempty"`
	FilterNicks *[]string `json:"filternicks,omitempty"`
}

var overridesMu sync.Mutex

// defaultModel is the model config.yml names, captured before any persisted override is layered on
// top.
var (
	defaultModelMu sync.RWMutex
	defaultModel   string
)

// DefaultModel returns the model named by config.yml, ignoring any runtime "+models" switch.
func DefaultModel() string {
	defaultModelMu.RLock()
	defer defaultModelMu.RUnlock()
	return defaultModel
}

func loadOverrides() runtimeOverrides {
	var o runtimeOverrides
	data, err := os.ReadFile(OverridesPath)
	if err != nil {
		return o
	}
	if err := json.Unmarshal(data, &o); err != nil {
		core.GetLogger().Warn("overrides_unreadable", "path", OverridesPath, "error", err.Error())
	}
	return o
}

// saveOverrides writes atomically so a crash mid-write can't leave a
// truncated file that silently drops every persisted setting.
func saveOverrides(o runtimeOverrides) {
	data, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		core.GetLogger().Error("overrides_marshal_failed", "error", err.Error())
		return
	}
	tmp := OverridesPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		core.GetLogger().Error("overrides_write_failed", "path", tmp, "error", err.Error())
		return
	}
	if err := os.Rename(tmp, OverridesPath); err != nil {
		core.GetLogger().Error("overrides_rename_failed", "path", OverridesPath, "error", err.Error())
		_ = os.Remove(tmp)
	}
}

// PersistSet records a "+set key value" change so it survives a restart.
func PersistSet(key, value string) {
	overridesMu.Lock()
	defer overridesMu.Unlock()

	o := loadOverrides()
	if o.Set == nil {
		o.Set = map[string]string{}
	}
	o.Set[key] = value
	saveOverrides(o)
}

// PersistUnset drops a persisted "+set" override, so the key reverts to whatever config.yml says.
func PersistUnset(key string) {
	overridesMu.Lock()
	defer overridesMu.Unlock()

	o := loadOverrides()
	if o.Set == nil {
		return
	}
	if _, ok := o.Set[key]; !ok {
		return
	}
	delete(o.Set, key)
	saveOverrides(o)
}

// PersistAdmins records the current admin list.
func PersistAdmins(admins []string) {
	overridesMu.Lock()
	defer overridesMu.Unlock()

	o := loadOverrides()
	// Store a copy: the caller's slice keeps mutating as admins are added.
	// Non-nil even when empty, so "no admins" persists rather than reverting.
	cp := append([]string{}, admins...)
	o.Admins = &cp
	saveOverrides(o)
}

// PersistScreening records the inbound and outbound screening lists.
func PersistScreening(screen, filter []string) {
	overridesMu.Lock()
	defer overridesMu.Unlock()
	o := loadOverrides()
	sc := append([]string{}, screen...)
	fi := append([]string{}, filter...)
	o.ScreenNicks = &sc
	o.FilterNicks = &fi
	saveOverrides(o)
}

// PersistAdminTools records which tools are restricted to admins.
func PersistAdminTools(adminTools []string) {
	overridesMu.Lock()
	defer overridesMu.Unlock()

	o := loadOverrides()
	cp := append([]string{}, adminTools...)
	o.AdminTools = &cp
	saveOverrides(o)
}

// ApplyOverrides layers persisted runtime changes over a freshly loaded config.
func ApplyOverrides(cfg *config.Configuration) []string {
	// Before anything is layered on, so this is config.yml's own value.
	defaultModelMu.Lock()
	defaultModel = cfg.Model.Model
	defaultModelMu.Unlock()

	overridesMu.Lock()
	o := loadOverrides()
	overridesMu.Unlock()

	for key, value := range o.Set {
		field, ok := configFields[key]
		if !ok {
			core.GetLogger().Warn("override_unknown_key", "key", key)
			continue
		}
		if err := field.setter(cfg, value); err != nil {
			// A stale or hand-edited value shouldn't stop the bot booting.
			core.GetLogger().Warn("override_rejected",
				"key", key, "value", value, "error", err.Error())
			continue
		}
		core.GetLogger().Info("override_applied", "key", key, "value", value)
	}

	if o.Admins != nil {
		cfg.Bot.Admins = *o.Admins
		core.GetLogger().Info("override_applied", "key", "admins", "count", len(*o.Admins))
	}
	if o.ScreenNicks != nil {
		cfg.Bot.ScreenNicks = *o.ScreenNicks
		core.GetLogger().Info("override_applied", "key", "screennicks", "count", len(*o.ScreenNicks))
	}
	if o.FilterNicks != nil {
		cfg.Bot.FilterNicks = *o.FilterNicks
		core.GetLogger().Info("override_applied", "key", "filternicks", "count", len(*o.FilterNicks))
	}
	if o.AdminTools != nil {
		cfg.Bot.AdminTools = *o.AdminTools
		core.GetLogger().Info("override_applied", "key", "admintools", "count", len(*o.AdminTools))
		return *o.AdminTools
	}
	return nil
}
