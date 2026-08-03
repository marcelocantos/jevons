// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"fmt"
	"sync"

	"github.com/marcelocantos/jevons/internal/config"
)

// ConfigManager holds the desired provider set from structured config and
// mirrors it into the persistence store (🎯T27.2). Reload re-reads the
// config file and replaces desired state without a daemon restart.
//
// Lifecycle reconcile (start/stop/connect processes) is 🎯T27.3 — this
// type only manages config + datastore. OnChange is the hook T27.3 will
// use when the desired set changes.
type ConfigManager struct {
	mu       sync.RWMutex
	cfgPath  string
	decls    []config.ProviderDecl
	store    *Store
	onChange func([]config.ProviderDecl) // optional; may be nil
}

// ConfigManagerArgs opens or attaches config + store.
type ConfigManagerArgs struct {
	// ConfigPath is the YAML path (typically config.DefaultPath()).
	ConfigPath string
	// Store is an already-open provider store. Required.
	Store *Store
	// OnChange is invoked after a successful Load/Reload with the new
	// desired set (copy). Nil is fine (T27.2 alone).
	OnChange func([]config.ProviderDecl)
}

// NewConfigManager loads config from path, persists desired providers,
// and returns a manager ready for Reload.
func NewConfigManager(args ConfigManagerArgs) (*ConfigManager, error) {
	if args.Store == nil {
		return nil, fmt.Errorf("provider.ConfigManager: Store is required")
	}
	if args.ConfigPath == "" {
		return nil, fmt.Errorf("provider.ConfigManager: ConfigPath is required")
	}
	m := &ConfigManager{
		cfgPath:  args.ConfigPath,
		store:    args.Store,
		onChange: args.OnChange,
	}
	if err := m.Reload(); err != nil {
		return nil, err
	}
	return m, nil
}

// Reload re-reads structured config and replaces the desired provider set
// in memory and in the store. Safe to call while the daemon is running
// (🎯T27.2 acceptance: reloadable without full restart). Malformed or
// invalid provider config is a hard error — the previous desired set is
// left unchanged on failure.
func (m *ConfigManager) Reload() error {
	cfg, err := config.Load(m.cfgPath)
	if err != nil {
		return err
	}
	// Copy decls so callers cannot mutate store-backed slices via Params maps.
	decls := cloneDecls(cfg.Providers)
	if err := m.store.ReplaceDesired(decls); err != nil {
		return err
	}
	m.mu.Lock()
	m.decls = decls
	cb := m.onChange
	m.mu.Unlock()
	if cb != nil {
		cb(cloneDecls(decls))
	}
	return nil
}

// Desired returns a copy of the current desired provider declarations.
func (m *ConfigManager) Desired() []config.ProviderDecl {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneDecls(m.decls)
}

// Enabled returns only enabled desired providers.
func (m *ConfigManager) Enabled() []config.ProviderDecl {
	all := m.Desired()
	out := make([]config.ProviderDecl, 0, len(all))
	for _, d := range all {
		if d.Enabled() {
			out = append(out, d)
		}
	}
	return out
}

// Store returns the underlying persistence home.
func (m *ConfigManager) Store() *Store { return m.store }

// ConfigPath returns the watched/reload config path.
func (m *ConfigManager) ConfigPath() string { return m.cfgPath }

// SetOnChange replaces the change hook (e.g. after T27.3 wires reconcile).
func (m *ConfigManager) SetOnChange(fn func([]config.ProviderDecl)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onChange = fn
}

func cloneDecls(in []config.ProviderDecl) []config.ProviderDecl {
	if len(in) == 0 {
		return nil
	}
	out := make([]config.ProviderDecl, len(in))
	for i, d := range in {
		out[i] = d
		if d.Params != nil {
			cp := make(map[string]any, len(d.Params))
			for k, v := range d.Params {
				cp[k] = v
			}
			out[i].Params = cp
		}
		if d.Enable != nil {
			b := *d.Enable
			out[i].Enable = &b
		}
	}
	return out
}
