// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package repowatch gives each Jevons PO standing awareness of its GitHub
// repo's issues and pull requests, polling `gh` at a bounded cadence (🎯T1022).
// It deliberately does not use bullseye's issuepipe webhook path (🎯T1022.1):
// this is the daemon-owned awareness half; bullseye_github_sync remains the
// PO's own accept-as-work mirror tool, and triage/escalation judgment lives
// in the PO (🎯T1022.3), not here.
package repowatch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Schedule and policy defaults. Mirrors internal/research's ambient-cycle
// shape (🎯T325 staff-cycle, not a permanent monologue): bounded cadence,
// bounded item counts, capacity-gated.
const (
	// DefaultInterval is the poll cadence, matching the research feed
	// interval per the 🎯T1022.2 design note (no owner-stated preference).
	DefaultInterval = 30 * time.Minute
	// DefaultMaxItemsPerRepo bounds items taken from one repo per cycle.
	DefaultMaxItemsPerRepo = 50
	// DirName is the repowatch subtree under state_dir.
	DirName = "repowatch"
	// ConfigFileName is the durable config under state_dir/repowatch.
	ConfigFileName = "config.json"
)

// RepoConfig is one watched repo's per-repo override. Empty fields fall back
// to the package Config default.
type RepoConfig struct {
	// Repo is "owner/name", matching the PO workdir's origin remote.
	Repo string `json:"repo"`
	// Enabled opts a repo out of the default-on watch (owner ask: default
	// yes, opt-out per repo — see 🎯T1022 design doc Fog map).
	Enabled *bool `json:"enabled,omitempty"`
	// Workdir is the PO's checkout this repo is watched from (gh needs a
	// repo checkout, or an explicit --repo, to resolve the right remote).
	Workdir string `json:"workdir,omitempty"`
	// PO is the owning agent name items are delivered to.
	PO string `json:"po,omitempty"`
}

// Enabled reports whether this repo is watched, defaulting to on.
func (r RepoConfig) EnabledOrDefault() bool {
	if r.Enabled == nil {
		return true
	}
	return *r.Enabled
}

// Config is the durable, overseer-retunable repowatch configuration.
type Config struct {
	Enabled         bool         `json:"enabled"`
	IntervalSec     int          `json:"interval_sec"`
	MaxItemsPerRepo int          `json:"max_items_per_repo"`
	Repos           []RepoConfig `json:"repos,omitempty"`

	UpdatedAt string `json:"updated_at,omitempty"`
	UpdatedBy string `json:"updated_by,omitempty"`
}

// DefaultConfig is the shipped posture: watching on, no repos configured
// until the daemon discovers PO workdirs or the owner adds one explicitly.
func DefaultConfig() Config {
	return Config{
		Enabled:         true,
		IntervalSec:     int(DefaultInterval / time.Second),
		MaxItemsPerRepo: DefaultMaxItemsPerRepo,
	}
}

// IntervalDuration is the poll cadence (negative disables the ticker).
func (c Config) IntervalDuration() time.Duration {
	if c.IntervalSec == 0 {
		return DefaultInterval
	}
	return time.Duration(c.IntervalSec) * time.Second
}

// EffectiveMaxItemsPerRepo bounds items taken from one repo per cycle.
func (c Config) EffectiveMaxItemsPerRepo() int {
	if c.MaxItemsPerRepo <= 0 {
		return DefaultMaxItemsPerRepo
	}
	return c.MaxItemsPerRepo
}

// RepoByName returns the per-repo override for repo, if configured.
func (c Config) RepoByName(repo string) (RepoConfig, bool) {
	want := strings.TrimSpace(repo)
	for _, r := range c.Repos {
		if strings.EqualFold(strings.TrimSpace(r.Repo), want) {
			return r, true
		}
	}
	return RepoConfig{}, false
}

// ConfigPath returns the durable config path for a state dir.
func ConfigPath(stateDir string) string {
	return filepath.Join(expandHome(stateDir), DirName, ConfigFileName)
}

// LoadConfig reads durable config, falling back to defaults when absent.
func LoadConfig(stateDir string) (Config, error) {
	path := ConfigPath(stateDir)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return Config{}, fmt.Errorf("repowatch config: read %s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		// Malformed durable state is a hard error, never a silent reset.
		return Config{}, fmt.Errorf("repowatch config: parse %s: %w", path, err)
	}
	return cfg, nil
}

// SaveConfig persists config atomically.
func SaveConfig(stateDir string, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("repowatch config: marshal: %w", err)
	}
	return writeFileAtomic(ConfigPath(stateDir), append(data, '\n'), 0o644)
}

// ConfigPatch is an overseer retune. Nil fields are left unchanged.
type ConfigPatch struct {
	Enabled         *bool
	IntervalSec     *int
	MaxItemsPerRepo *int
	// SetRepo adds or updates one repo's override.
	SetRepo *RepoConfig
	// RemoveRepo drops a repo's override (it falls back to defaults, still
	// watched unless re-added disabled).
	RemoveRepo string
	UpdatedBy  string
	Now        time.Time
}

// PatchConfig applies a retune and persists the result.
func PatchConfig(stateDir string, patch ConfigPatch) (Config, error) {
	cfg, err := LoadConfig(stateDir)
	if err != nil {
		return Config{}, err
	}
	if patch.Enabled != nil {
		cfg.Enabled = *patch.Enabled
	}
	if patch.IntervalSec != nil {
		cfg.IntervalSec = *patch.IntervalSec
	}
	if patch.MaxItemsPerRepo != nil {
		cfg.MaxItemsPerRepo = *patch.MaxItemsPerRepo
	}
	if patch.SetRepo != nil {
		rc := *patch.SetRepo
		rc.Repo = strings.TrimSpace(rc.Repo)
		if rc.Repo == "" {
			return Config{}, fmt.Errorf("repowatch config: repo name required")
		}
		replaced := false
		for i, r := range cfg.Repos {
			if strings.EqualFold(r.Repo, rc.Repo) {
				cfg.Repos[i] = rc
				replaced = true
				break
			}
		}
		if !replaced {
			cfg.Repos = append(cfg.Repos, rc)
		}
	}
	if name := strings.TrimSpace(patch.RemoveRepo); name != "" {
		kept := cfg.Repos[:0]
		for _, r := range cfg.Repos {
			if !strings.EqualFold(r.Repo, name) {
				kept = append(kept, r)
			}
		}
		cfg.Repos = append([]RepoConfig(nil), kept...)
	}
	now := patch.Now
	if now.IsZero() {
		now = time.Now()
	}
	cfg.UpdatedAt = now.UTC().Format(time.RFC3339)
	if by := strings.TrimSpace(patch.UpdatedBy); by != "" {
		cfg.UpdatedBy = by
	}
	if err := SaveConfig(stateDir, cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
