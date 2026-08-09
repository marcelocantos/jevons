// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package rsi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CoachConfig is overseer-retunable coach runtime state (🎯T243 bi-directional SI).
// Stored at state_dir/rsi/coach_config.json. Alert fatigue is a dial, not a veto.
type CoachConfig struct {
	// Enabled when false skips schedule delivery (still allows manual cycle dry-run).
	Enabled bool `json:"enabled"`
	// Interval is schedule period (0 = package default; negative disables ticker).
	IntervalSec int `json:"interval_sec"`
	// RateCap max judgments delivered per cycle (0 = default).
	RateCap int `json:"rate_cap"`
	// MinCount frequency threshold for extract (0 = default).
	MinCount int `json:"min_count"`
	// FocusFilters optional kind/component/message substrings.
	FocusFilters []string `json:"focus_filters,omitempty"`
	// SystemPrompt overrides DefaultCoachSystemPrompt when non-empty.
	SystemPrompt string `json:"system_prompt,omitempty"`
	// Overseer is the deliver target name (default: jevons).
	Overseer string `json:"overseer,omitempty"`
	// CoachName is the durable coach identity (default: rsi-coach).
	CoachName string `json:"coach_name,omitempty"`
	// UpdatedAt is last overseer retune time (RFC3339).
	UpdatedAt string `json:"updated_at,omitempty"`
	// UpdatedBy records who retuned (e.g. jevons).
	UpdatedBy string `json:"updated_by,omitempty"`

	// Retrospective history dials (🎯T353). The drip dials above govern the
	// forward cursor; these govern the bounded backward pass. They are separate
	// on purpose: observe fine and often, conclude coarse and rarely.

	// RetroEnabled turns the scheduled retrospective pass on (default true).
	RetroEnabled bool `json:"retro_enabled"`
	// RetroIntervalSec is the retro schedule period (0 = 6h default; negative
	// disables the retro ticker — MCP-triggered passes still work).
	RetroIntervalSec int `json:"retro_interval_sec"`
	// RetroLookbackHours is how far back one pass reaches (0 = 168h / 7d).
	// This is the whole window: a pass never scans beyond it, so history is
	// never re-read in full on the drip cadence.
	RetroLookbackHours int `json:"retro_lookback_hours"`
	// RetroRateCap is max judgments delivered per retro pass (0 = 2).
	RetroRateCap int `json:"retro_rate_cap"`
	// RetroMinCount is the coarse-conclusion threshold applied by the retro
	// value bar after fine-grained extraction (0 = 3).
	RetroMinCount int `json:"retro_min_count"`
	// RetroMaxCommits / RetroMaxEventRows / RetroMaxSessions /
	// RetroMaxTurnsPerSession bound each surface per pass (0 = defaults).
	RetroMaxCommits         int `json:"retro_max_commits"`
	RetroMaxEventRows       int `json:"retro_max_event_rows"`
	RetroMaxSessions        int `json:"retro_max_sessions"`
	RetroMaxTurnsPerSession int `json:"retro_max_turns_per_session"`
	// RetroWorkdir overrides the git repository mined for history (empty = the
	// daemon's configured workdir; non-git paths simply yield no git evidence).
	RetroWorkdir string `json:"retro_workdir,omitempty"`
}

// DefaultCoachConfig returns production defaults.
func DefaultCoachConfig() CoachConfig {
	return CoachConfig{
		Enabled:                 true,
		IntervalSec:             int(DefaultCoachInterval / time.Second),
		RateCap:                 DefaultCoachRateCap,
		MinCount:                DefaultMinCount,
		Overseer:                "jevons",
		CoachName:               DefaultCoachName,
		RetroEnabled:            true,
		RetroIntervalSec:        int(DefaultRetroInterval / time.Second),
		RetroLookbackHours:      int(DefaultRetroLookback / time.Hour),
		RetroRateCap:            DefaultRetroRateCap,
		RetroMinCount:           DefaultRetroMinCount,
		RetroMaxCommits:         DefaultRetroMaxCommits,
		RetroMaxEventRows:       DefaultRetroMaxEventRows,
		RetroMaxSessions:        DefaultRetroMaxSessions,
		RetroMaxTurnsPerSession: DefaultRetroTurnCap,
	}
}

// CoachConfigPath is stateDir/rsi/coach_config.json.
func CoachConfigPath(stateDir string) string {
	return filepath.Join(stateDir, "rsi", "coach_config.json")
}

// coachConfigWire is the on-disk shape with pointers so explicit false/0 survive.
type coachConfigWire struct {
	Enabled      *bool    `json:"enabled"`
	IntervalSec  *int     `json:"interval_sec"`
	RateCap      *int     `json:"rate_cap"`
	MinCount     *int     `json:"min_count"`
	FocusFilters []string `json:"focus_filters,omitempty"`
	SystemPrompt *string  `json:"system_prompt,omitempty"`
	Overseer     *string  `json:"overseer,omitempty"`
	CoachName    *string  `json:"coach_name,omitempty"`
	UpdatedAt    string   `json:"updated_at,omitempty"`
	UpdatedBy    string   `json:"updated_by,omitempty"`

	RetroEnabled            *bool   `json:"retro_enabled"`
	RetroIntervalSec        *int    `json:"retro_interval_sec"`
	RetroLookbackHours      *int    `json:"retro_lookback_hours"`
	RetroRateCap            *int    `json:"retro_rate_cap"`
	RetroMinCount           *int    `json:"retro_min_count"`
	RetroMaxCommits         *int    `json:"retro_max_commits"`
	RetroMaxEventRows       *int    `json:"retro_max_event_rows"`
	RetroMaxSessions        *int    `json:"retro_max_sessions"`
	RetroMaxTurnsPerSession *int    `json:"retro_max_turns_per_session"`
	RetroWorkdir            *string `json:"retro_workdir,omitempty"`
}

// LoadCoachConfig reads durable config; missing file → defaults.
func LoadCoachConfig(stateDir string) (CoachConfig, error) {
	cfg := DefaultCoachConfig()
	path := CoachConfigPath(stateDir)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("rsi coach config: read: %w", err)
	}
	if len(bytesTrimSpace(data)) == 0 {
		return cfg, nil
	}
	var w coachConfigWire
	if err := json.Unmarshal(data, &w); err != nil {
		return cfg, fmt.Errorf("rsi coach config: parse %s: %w", path, err)
	}
	if w.Enabled != nil {
		cfg.Enabled = *w.Enabled
	}
	if w.IntervalSec != nil {
		cfg.IntervalSec = *w.IntervalSec
	}
	if w.RateCap != nil {
		cfg.RateCap = *w.RateCap
	}
	if w.MinCount != nil {
		cfg.MinCount = *w.MinCount
	}
	if w.FocusFilters != nil {
		cfg.FocusFilters = append([]string{}, w.FocusFilters...)
	}
	if w.SystemPrompt != nil {
		cfg.SystemPrompt = *w.SystemPrompt
	}
	if w.Overseer != nil {
		cfg.Overseer = *w.Overseer
	}
	if w.CoachName != nil {
		cfg.CoachName = *w.CoachName
	}
	if w.UpdatedAt != "" {
		cfg.UpdatedAt = w.UpdatedAt
	}
	if w.UpdatedBy != "" {
		cfg.UpdatedBy = w.UpdatedBy
	}
	if w.RetroEnabled != nil {
		cfg.RetroEnabled = *w.RetroEnabled
	}
	if w.RetroIntervalSec != nil {
		cfg.RetroIntervalSec = *w.RetroIntervalSec
	}
	if w.RetroLookbackHours != nil {
		cfg.RetroLookbackHours = *w.RetroLookbackHours
	}
	if w.RetroRateCap != nil {
		cfg.RetroRateCap = *w.RetroRateCap
	}
	if w.RetroMinCount != nil {
		cfg.RetroMinCount = *w.RetroMinCount
	}
	if w.RetroMaxCommits != nil {
		cfg.RetroMaxCommits = *w.RetroMaxCommits
	}
	if w.RetroMaxEventRows != nil {
		cfg.RetroMaxEventRows = *w.RetroMaxEventRows
	}
	if w.RetroMaxSessions != nil {
		cfg.RetroMaxSessions = *w.RetroMaxSessions
	}
	if w.RetroMaxTurnsPerSession != nil {
		cfg.RetroMaxTurnsPerSession = *w.RetroMaxTurnsPerSession
	}
	if w.RetroWorkdir != nil {
		cfg.RetroWorkdir = *w.RetroWorkdir
	}
	return cfg, nil
}

// SaveCoachConfig atomically writes config.
func SaveCoachConfig(stateDir string, cfg CoachConfig) error {
	if strings.TrimSpace(stateDir) == "" {
		return fmt.Errorf("rsi coach config: stateDir required")
	}
	dir := filepath.Join(stateDir, "rsi")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("rsi coach config: mkdir: %w", err)
	}
	path := CoachConfigPath(stateDir)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("rsi coach config: marshal: %w", err)
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("rsi coach config: write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rsi coach config: rename: %w", err)
	}
	return nil
}

// ApplyCoachConfigPatch merges non-nil patch fields into base (overseer retune).
// Pointer fields distinguish "unset" from explicit zero for some dials.
type CoachConfigPatch struct {
	Enabled      *bool
	IntervalSec  *int
	RateCap      *int
	MinCount     *int
	FocusFilters *[]string
	SystemPrompt *string
	Overseer     *string
	CoachName    *string
	UpdatedBy    string
	Now          time.Time

	// Retrospective dials (🎯T353).
	RetroEnabled            *bool
	RetroIntervalSec        *int
	RetroLookbackHours      *int
	RetroRateCap            *int
	RetroMinCount           *int
	RetroMaxCommits         *int
	RetroMaxEventRows       *int
	RetroMaxSessions        *int
	RetroMaxTurnsPerSession *int
	RetroWorkdir            *string
}

// PatchCoachConfig applies a patch and persists.
func PatchCoachConfig(stateDir string, patch CoachConfigPatch) (CoachConfig, error) {
	cfg, err := LoadCoachConfig(stateDir)
	if err != nil {
		return cfg, err
	}
	if patch.Enabled != nil {
		cfg.Enabled = *patch.Enabled
	}
	if patch.IntervalSec != nil {
		cfg.IntervalSec = *patch.IntervalSec
	}
	if patch.RateCap != nil {
		cfg.RateCap = *patch.RateCap
	}
	if patch.MinCount != nil {
		cfg.MinCount = *patch.MinCount
	}
	if patch.FocusFilters != nil {
		cfg.FocusFilters = append([]string{}, (*patch.FocusFilters)...)
	}
	if patch.SystemPrompt != nil {
		cfg.SystemPrompt = *patch.SystemPrompt
	}
	if patch.Overseer != nil {
		cfg.Overseer = strings.TrimSpace(*patch.Overseer)
	}
	if patch.CoachName != nil {
		cfg.CoachName = strings.TrimSpace(*patch.CoachName)
	}
	if patch.RetroEnabled != nil {
		cfg.RetroEnabled = *patch.RetroEnabled
	}
	if patch.RetroIntervalSec != nil {
		cfg.RetroIntervalSec = *patch.RetroIntervalSec
	}
	if patch.RetroLookbackHours != nil {
		cfg.RetroLookbackHours = *patch.RetroLookbackHours
	}
	if patch.RetroRateCap != nil {
		cfg.RetroRateCap = *patch.RetroRateCap
	}
	if patch.RetroMinCount != nil {
		cfg.RetroMinCount = *patch.RetroMinCount
	}
	if patch.RetroMaxCommits != nil {
		cfg.RetroMaxCommits = *patch.RetroMaxCommits
	}
	if patch.RetroMaxEventRows != nil {
		cfg.RetroMaxEventRows = *patch.RetroMaxEventRows
	}
	if patch.RetroMaxSessions != nil {
		cfg.RetroMaxSessions = *patch.RetroMaxSessions
	}
	if patch.RetroMaxTurnsPerSession != nil {
		cfg.RetroMaxTurnsPerSession = *patch.RetroMaxTurnsPerSession
	}
	if patch.RetroWorkdir != nil {
		cfg.RetroWorkdir = strings.TrimSpace(*patch.RetroWorkdir)
	}
	now := patch.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	cfg.UpdatedAt = now.Format(time.RFC3339)
	if by := strings.TrimSpace(patch.UpdatedBy); by != "" {
		cfg.UpdatedBy = by
	}
	if err := SaveCoachConfig(stateDir, cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// IntervalDuration maps IntervalSec to time.Duration (0 → default).
func (c CoachConfig) IntervalDuration() time.Duration {
	if c.IntervalSec < 0 {
		return -1
	}
	if c.IntervalSec == 0 {
		return DefaultCoachInterval
	}
	return time.Duration(c.IntervalSec) * time.Second
}

// EffectiveSystemPrompt returns configured or default prompt.
func (c CoachConfig) EffectiveSystemPrompt() string {
	if s := strings.TrimSpace(c.SystemPrompt); s != "" {
		return s
	}
	return DefaultCoachSystemPrompt()
}

// EffectiveOverseer returns deliver target.
func (c CoachConfig) EffectiveOverseer() string {
	if s := strings.TrimSpace(c.Overseer); s != "" {
		return s
	}
	return "jevons"
}

// EffectiveCoachName returns coach identity.
func (c CoachConfig) EffectiveCoachName() string {
	if s := strings.TrimSpace(c.CoachName); s != "" {
		return s
	}
	return DefaultCoachName
}

// EffectiveRateCap returns rate cap with default.
func (c CoachConfig) EffectiveRateCap() int {
	if c.RateCap <= 0 {
		return DefaultCoachRateCap
	}
	return c.RateCap
}

// EffectiveMinCount returns min count with default.
func (c CoachConfig) EffectiveMinCount() int {
	if c.MinCount <= 0 {
		return DefaultMinCount
	}
	return c.MinCount
}

// RetroIntervalDuration maps RetroIntervalSec to a duration
// (0 → default; negative → -1, meaning "no retro ticker").
func (c CoachConfig) RetroIntervalDuration() time.Duration {
	if c.RetroIntervalSec < 0 {
		return -1
	}
	if c.RetroIntervalSec == 0 {
		return DefaultRetroInterval
	}
	return time.Duration(c.RetroIntervalSec) * time.Second
}

// RetroLookback is the bounded window one retrospective pass reaches back over.
func (c CoachConfig) RetroLookback() time.Duration {
	if c.RetroLookbackHours <= 0 {
		return DefaultRetroLookback
	}
	return time.Duration(c.RetroLookbackHours) * time.Hour
}

// EffectiveRetroRateCap returns the retro delivery cap with default.
func (c CoachConfig) EffectiveRetroRateCap() int {
	if c.RetroRateCap <= 0 {
		return DefaultRetroRateCap
	}
	return c.RetroRateCap
}

// EffectiveRetroMinCount returns the coarse-conclusion threshold with default.
func (c CoachConfig) EffectiveRetroMinCount() int {
	if c.RetroMinCount <= 0 {
		return DefaultRetroMinCount
	}
	return c.RetroMinCount
}

// RetroWindowAt builds the bounded pass window from the dials.
func (c CoachConfig) RetroWindowAt(now time.Time) RetroWindow {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return RetroWindow{
		Since:              now.Add(-c.RetroLookback()),
		Now:                now,
		MaxCommits:         c.RetroMaxCommits,
		MaxEventRows:       c.RetroMaxEventRows,
		MaxSessions:        c.RetroMaxSessions,
		MaxTurnsPerSession: c.RetroMaxTurnsPerSession,
	}
}
