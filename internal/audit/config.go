// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package audit is the periodic full-scan auditor (🎯T357): a bounded,
// scheduled cycle that scopes product code, skills trees, and agent
// prompts, hands the bounded manifest to an advanced-model auditor, and
// folds the resulting findings into durable residue.
//
// Shape is a staff cycle, not a permanent monologue auditor: every pass is
// bounded (scope caps, byte caps, wall-clock timeout, cycles-per-day), and
// findings land as durable artifacts (reports + residue) rather than chat
// noise. Critical findings notify the overseer in the same cycle.
//
// The package never rewrites source and never files bullseye targets
// itself: it suggests targets, the overseer disposes (mirrors 🎯T243).
package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Schedule, scope, and cost bounds. Every constant here exists so one pass
// stays bounded in scope, time, and spend.
const (
	// DefaultInterval is the full-scan cadence. Audits are expensive and
	// slow-moving: daily is plenty, and the cycles-per-day cap backstops it.
	DefaultInterval = 24 * time.Hour
	// DefaultTimeout bounds one auditor pass end to end.
	DefaultTimeout = 20 * time.Minute
	// DefaultMaxFilesPerScope bounds files listed per scope kind per pass.
	DefaultMaxFilesPerScope = 400
	// DefaultMaxBytesPerScope bounds manifest bytes per scope kind per pass.
	DefaultMaxBytesPerScope = 4 << 20
	// DefaultMaxFileBytes skips individual files larger than this.
	DefaultMaxFileBytes = 256 << 10
	// DefaultMaxFindings caps findings accepted from one report.
	DefaultMaxFindings = 40
	// DefaultMaxOutputBytes caps bytes read back from the auditor.
	DefaultMaxOutputBytes = 1 << 20
	// DefaultMaxCyclesPerDay is the standing cost guard on scheduled passes.
	DefaultMaxCyclesPerDay = 4
	// DefaultMinCycleGap prevents manual-trigger thrash.
	DefaultMinCycleGap = 5 * time.Minute
	// DefaultKeepReports bounds retained report artifacts on disk.
	DefaultKeepReports = 30

	// ConfigFileName is the durable config under state_dir/audit.
	ConfigFileName = "config.json"
	// StateFileName records last-cycle bookkeeping and the cost window.
	StateFileName = "state.json"
	// ResidueFileName is the durable finding residue ledger.
	ResidueFileName = "residue.json"
	// ReportsDirName holds per-cycle report artifacts.
	ReportsDirName = "reports"
	// DirName is the package state directory under state_dir.
	DirName = "audit"

	// DefaultAuditorName is the durable auditor identity handle.
	DefaultAuditorName = "code-auditor"
	// EventSource stamps overseer-directed audit deliveries.
	EventSource = "code-auditor"
	// Component is the eventlog component for dual-write filters.
	Component = "code_auditor"
	// DefaultOverseer is the default notify target.
	DefaultOverseer = "jevons"
)

// Advanced-tier auditor pin (🎯T357 acceptance #2). The audit cycle is
// deliberately not run on the cheap default tier: a full-scan audit is a
// low-frequency, high-leverage read, so it is pinned to an advanced model
// and that pin is documented in durable config rather than hardcoded at
// the call site. Owner ratification of broader Fable-class defaults for
// root/POs is a separate decision (🎯T358); this pin covers audits only.
const (
	// DefaultProvider is the auditor backend.
	DefaultProvider = "claude"
	// DefaultModel is the advanced-tier pin used for audit passes.
	DefaultModel = "claude-fable-5"
)

// DefaultCommand is the headless advanced-model invocation. The literal
// "{model}" is substituted with the configured model at dispatch. The
// auditor reads the manifest files itself (it has file tools) and answers
// with the JSON report; the prompt arrives on stdin.
func DefaultCommand() []string {
	return []string{"claude", "-p", "--model", "{model}"}
}

// Config is the durable, overseer-retunable auditor configuration.
type Config struct {
	// Enabled gates the schedule. Manual cycles still run when false.
	Enabled bool `json:"enabled"`
	// IntervalSec is the schedule period (0 = default; negative disables
	// the ticker while leaving manual cycles available).
	IntervalSec int `json:"interval_sec"`

	// Provider / Model pin the auditor tier (advanced by default).
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	// Command is the auditor invocation; "{model}" is substituted.
	Command []string `json:"command,omitempty"`
	// Workdir is the directory the auditor runs in (repo root).
	Workdir string `json:"workdir,omitempty"`

	// Scope roots per kind. Entries may be directories or single files;
	// missing roots are reported, not fatal.
	CodeRoots   []string `json:"code_roots,omitempty"`
	SkillsRoots []string `json:"skills_roots,omitempty"`
	PromptRoots []string `json:"prompt_roots,omitempty"`

	// Bounds on one pass.
	TimeoutSec       int `json:"timeout_sec"`
	MaxFilesPerScope int `json:"max_files_per_scope"`
	MaxBytesPerScope int `json:"max_bytes_per_scope"`
	MaxFileBytes     int `json:"max_file_bytes"`
	MaxFindings      int `json:"max_findings"`
	MaxOutputBytes   int `json:"max_output_bytes"`
	MaxCyclesPerDay  int `json:"max_cycles_per_day"`
	MinCycleGapSec   int `json:"min_cycle_gap_sec"`
	KeepReports      int `json:"keep_reports"`

	// Deliver posts critical findings to the overseer in-cycle.
	Deliver bool `json:"deliver"`
	// Overseer is the deliver target name.
	Overseer string `json:"overseer,omitempty"`
	// NotifySeverity is the minimum severity that notifies (default critical).
	NotifySeverity Severity `json:"notify_severity,omitempty"`
	// MaxNotifiesPerCycle caps in-cycle notifications.
	MaxNotifiesPerCycle int `json:"max_notifies_per_cycle"`

	// SystemPrompt overrides the default auditor brief when non-empty.
	SystemPrompt string `json:"system_prompt,omitempty"`

	// UpdatedAt / UpdatedBy record overseer retune provenance.
	UpdatedAt string `json:"updated_at,omitempty"`
	UpdatedBy string `json:"updated_by,omitempty"`
}

// DefaultConfig returns production defaults rooted at workdir and home.
// Roots that do not exist on this machine are harmless: the scan reports
// them as missing rather than failing the pass.
func DefaultConfig(workdir, home string) Config {
	workdir = strings.TrimSpace(workdir)
	home = strings.TrimSpace(home)
	cfg := Config{
		Enabled:             true,
		IntervalSec:         int(DefaultInterval / time.Second),
		Provider:            DefaultProvider,
		Model:               DefaultModel,
		Command:             DefaultCommand(),
		Workdir:             workdir,
		TimeoutSec:          int(DefaultTimeout / time.Second),
		MaxFilesPerScope:    DefaultMaxFilesPerScope,
		MaxBytesPerScope:    DefaultMaxBytesPerScope,
		MaxFileBytes:        DefaultMaxFileBytes,
		MaxFindings:         DefaultMaxFindings,
		MaxOutputBytes:      DefaultMaxOutputBytes,
		MaxCyclesPerDay:     DefaultMaxCyclesPerDay,
		MinCycleGapSec:      int(DefaultMinCycleGap / time.Second),
		KeepReports:         DefaultKeepReports,
		Deliver:             true,
		Overseer:            DefaultOverseer,
		NotifySeverity:      SeverityCritical,
		MaxNotifiesPerCycle: 3,
	}
	if workdir != "" {
		cfg.CodeRoots = []string{
			filepath.Join(workdir, "cmd"),
			filepath.Join(workdir, "internal"),
			filepath.Join(workdir, "web", "scripts"),
			filepath.Join(workdir, "scripts"),
		}
		cfg.PromptRoots = []string{
			filepath.Join(workdir, "internal", "config", "persona.md"),
			filepath.Join(workdir, "agents-guide.md"),
			filepath.Join(workdir, "AGENTS.md"),
			filepath.Join(workdir, "CLAUDE.md"),
		}
		cfg.SkillsRoots = []string{filepath.Join(workdir, ".claude", "skills")}
	}
	if home != "" {
		cfg.SkillsRoots = append(cfg.SkillsRoots, filepath.Join(home, ".claude", "skills"))
		cfg.PromptRoots = append(cfg.PromptRoots,
			filepath.Join(home, ".claude", "AGENTS.md"),
			filepath.Join(home, ".claude", "CLAUDE.md"),
		)
	}
	return cfg
}

// RootsFor returns the configured roots for a scope kind.
func (c Config) RootsFor(kind ScopeKind) []string {
	switch kind {
	case ScopeCode:
		return c.CodeRoots
	case ScopeSkills:
		return c.SkillsRoots
	case ScopePrompts:
		return c.PromptRoots
	}
	return nil
}

// IntervalDuration maps IntervalSec to a duration (0 → default;
// negative → -1, meaning "no ticker").
func (c Config) IntervalDuration() time.Duration {
	if c.IntervalSec < 0 {
		return -1
	}
	if c.IntervalSec == 0 {
		return DefaultInterval
	}
	return time.Duration(c.IntervalSec) * time.Second
}

// Timeout bounds one auditor pass.
func (c Config) Timeout() time.Duration {
	if c.TimeoutSec <= 0 {
		return DefaultTimeout
	}
	return time.Duration(c.TimeoutSec) * time.Second
}

// MinCycleGap is the anti-thrash floor between cycles.
func (c Config) MinCycleGap() time.Duration {
	if c.MinCycleGapSec < 0 {
		return 0
	}
	if c.MinCycleGapSec == 0 {
		return DefaultMinCycleGap
	}
	return time.Duration(c.MinCycleGapSec) * time.Second
}

// EffectiveProvider returns the auditor backend with default.
func (c Config) EffectiveProvider() string {
	if s := strings.TrimSpace(c.Provider); s != "" {
		return s
	}
	return DefaultProvider
}

// EffectiveModel returns the advanced-tier model pin with default.
func (c Config) EffectiveModel() string {
	if s := strings.TrimSpace(c.Model); s != "" {
		return s
	}
	return DefaultModel
}

// EffectiveCommand returns the auditor invocation with "{model}" bound.
func (c Config) EffectiveCommand() []string {
	src := c.Command
	if len(src) == 0 {
		src = DefaultCommand()
	}
	out := make([]string, 0, len(src))
	for _, arg := range src {
		out = append(out, strings.ReplaceAll(arg, "{model}", c.EffectiveModel()))
	}
	return out
}

// EffectiveOverseer returns the deliver target.
func (c Config) EffectiveOverseer() string {
	if s := strings.TrimSpace(c.Overseer); s != "" {
		return s
	}
	return DefaultOverseer
}

// EffectiveNotifySeverity returns the notify threshold with default.
func (c Config) EffectiveNotifySeverity() Severity {
	if s := NormalizeSeverity(string(c.NotifySeverity)); s != "" {
		return s
	}
	return SeverityCritical
}

// Bounds projects the scan-shaping dials, applying defaults.
func (c Config) Bounds() Bounds {
	b := Bounds{
		MaxFilesPerScope: c.MaxFilesPerScope,
		MaxBytesPerScope: int64(c.MaxBytesPerScope),
		MaxFileBytes:     int64(c.MaxFileBytes),
	}
	if b.MaxFilesPerScope <= 0 {
		b.MaxFilesPerScope = DefaultMaxFilesPerScope
	}
	if b.MaxBytesPerScope <= 0 {
		b.MaxBytesPerScope = DefaultMaxBytesPerScope
	}
	if b.MaxFileBytes <= 0 {
		b.MaxFileBytes = DefaultMaxFileBytes
	}
	return b
}

// EffectiveMaxFindings caps findings accepted from one report.
func (c Config) EffectiveMaxFindings() int {
	if c.MaxFindings <= 0 {
		return DefaultMaxFindings
	}
	return c.MaxFindings
}

// EffectiveMaxOutputBytes caps auditor output read back.
func (c Config) EffectiveMaxOutputBytes() int {
	if c.MaxOutputBytes <= 0 {
		return DefaultMaxOutputBytes
	}
	return c.MaxOutputBytes
}

// EffectiveMaxCyclesPerDay is the standing cost guard.
func (c Config) EffectiveMaxCyclesPerDay() int {
	if c.MaxCyclesPerDay < 0 {
		return 0
	}
	if c.MaxCyclesPerDay == 0 {
		return DefaultMaxCyclesPerDay
	}
	return c.MaxCyclesPerDay
}

// EffectiveKeepReports bounds retained report artifacts.
func (c Config) EffectiveKeepReports() int {
	if c.KeepReports <= 0 {
		return DefaultKeepReports
	}
	return c.KeepReports
}

// EffectiveMaxNotifies caps in-cycle notifications.
func (c Config) EffectiveMaxNotifies() int {
	if c.MaxNotifiesPerCycle <= 0 {
		return 3
	}
	return c.MaxNotifiesPerCycle
}

// ConfigPath is stateDir/audit/config.json.
func ConfigPath(stateDir string) string {
	return filepath.Join(stateDir, DirName, ConfigFileName)
}

// LoadConfig reads durable config; missing file → defaults for workdir/home.
// Malformed config is a hard error, never a silent reset.
func LoadConfig(stateDir, workdir, home string) (Config, error) {
	cfg := DefaultConfig(workdir, home)
	path := ConfigPath(stateDir)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("audit config: read: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return cfg, nil
	}
	// Unmarshal over the defaults: absent fields keep their default, present
	// fields win — including explicit false / 0.
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("audit config: parse %s: %w", path, err)
	}
	if strings.TrimSpace(cfg.Workdir) == "" {
		cfg.Workdir = workdir
	}
	return cfg, nil
}

// SaveConfig atomically writes config.
func SaveConfig(stateDir string, cfg Config) error {
	if strings.TrimSpace(stateDir) == "" {
		return fmt.Errorf("audit config: stateDir required")
	}
	dir := filepath.Join(stateDir, DirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("audit config: mkdir: %w", err)
	}
	return writeJSONAtomic(ConfigPath(stateDir), cfg)
}

// ConfigPatch is an overseer retune. Nil fields are left untouched so
// explicit false / 0 survives a patch.
type ConfigPatch struct {
	Enabled             *bool
	IntervalSec         *int
	Provider            *string
	Model               *string
	Command             *[]string
	Workdir             *string
	CodeRoots           *[]string
	SkillsRoots         *[]string
	PromptRoots         *[]string
	TimeoutSec          *int
	MaxFilesPerScope    *int
	MaxBytesPerScope    *int
	MaxFileBytes        *int
	MaxFindings         *int
	MaxOutputBytes      *int
	MaxCyclesPerDay     *int
	MinCycleGapSec      *int
	KeepReports         *int
	Deliver             *bool
	Overseer            *string
	NotifySeverity      *string
	MaxNotifiesPerCycle *int
	SystemPrompt        *string

	UpdatedBy string
	Now       time.Time
}

// PatchConfig applies a retune and persists it.
func PatchConfig(stateDir, workdir, home string, patch ConfigPatch) (Config, error) {
	cfg, err := LoadConfig(stateDir, workdir, home)
	if err != nil {
		return cfg, err
	}
	if patch.Enabled != nil {
		cfg.Enabled = *patch.Enabled
	}
	if patch.IntervalSec != nil {
		cfg.IntervalSec = *patch.IntervalSec
	}
	if patch.Provider != nil {
		cfg.Provider = strings.TrimSpace(*patch.Provider)
	}
	if patch.Model != nil {
		cfg.Model = strings.TrimSpace(*patch.Model)
	}
	if patch.Command != nil {
		cfg.Command = append([]string{}, (*patch.Command)...)
	}
	if patch.Workdir != nil {
		cfg.Workdir = strings.TrimSpace(*patch.Workdir)
	}
	if patch.CodeRoots != nil {
		cfg.CodeRoots = append([]string{}, (*patch.CodeRoots)...)
	}
	if patch.SkillsRoots != nil {
		cfg.SkillsRoots = append([]string{}, (*patch.SkillsRoots)...)
	}
	if patch.PromptRoots != nil {
		cfg.PromptRoots = append([]string{}, (*patch.PromptRoots)...)
	}
	if patch.TimeoutSec != nil {
		cfg.TimeoutSec = *patch.TimeoutSec
	}
	if patch.MaxFilesPerScope != nil {
		cfg.MaxFilesPerScope = *patch.MaxFilesPerScope
	}
	if patch.MaxBytesPerScope != nil {
		cfg.MaxBytesPerScope = *patch.MaxBytesPerScope
	}
	if patch.MaxFileBytes != nil {
		cfg.MaxFileBytes = *patch.MaxFileBytes
	}
	if patch.MaxFindings != nil {
		cfg.MaxFindings = *patch.MaxFindings
	}
	if patch.MaxOutputBytes != nil {
		cfg.MaxOutputBytes = *patch.MaxOutputBytes
	}
	if patch.MaxCyclesPerDay != nil {
		cfg.MaxCyclesPerDay = *patch.MaxCyclesPerDay
	}
	if patch.MinCycleGapSec != nil {
		cfg.MinCycleGapSec = *patch.MinCycleGapSec
	}
	if patch.KeepReports != nil {
		cfg.KeepReports = *patch.KeepReports
	}
	if patch.Deliver != nil {
		cfg.Deliver = *patch.Deliver
	}
	if patch.Overseer != nil {
		cfg.Overseer = strings.TrimSpace(*patch.Overseer)
	}
	if patch.NotifySeverity != nil {
		if s := NormalizeSeverity(*patch.NotifySeverity); s != "" {
			cfg.NotifySeverity = s
		}
	}
	if patch.MaxNotifiesPerCycle != nil {
		cfg.MaxNotifiesPerCycle = *patch.MaxNotifiesPerCycle
	}
	if patch.SystemPrompt != nil {
		cfg.SystemPrompt = *patch.SystemPrompt
	}
	now := patch.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	cfg.UpdatedAt = now.Format(time.RFC3339)
	if by := strings.TrimSpace(patch.UpdatedBy); by != "" {
		cfg.UpdatedBy = by
	}
	if err := SaveConfig(stateDir, cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// State is durable bookkeeping across cycles: last pass and the rolling
// cost window that enforces MaxCyclesPerDay.
type State struct {
	LastCycleAt   time.Time   `json:"last_cycle_at,omitzero"`
	LastReportID  string      `json:"last_report_id,omitempty"`
	LastReason    string      `json:"last_reason,omitempty"`
	LastError     string      `json:"last_error,omitempty"`
	CycleCount    int         `json:"cycle_count"`
	RecentCycles  []time.Time `json:"recent_cycles,omitempty"`
	LastFindings  int         `json:"last_findings"`
	LastNotified  int         `json:"last_notified"`
	LastModel     string      `json:"last_model,omitempty"`
	LastDurationS float64     `json:"last_duration_sec,omitempty"`
	// LastTier records the capacity tier the last pass ran at (🎯T359).
	LastTier string `json:"last_tier,omitempty"`
	// LastSkipReason records why the most recent scheduled tick did not
	// dispatch — a deferred audit must be answerable from state, not
	// indistinguishable from a clean one.
	LastSkipReason string `json:"last_skip_reason,omitempty"`
}

// StatePath is stateDir/audit/state.json.
func StatePath(stateDir string) string {
	return filepath.Join(stateDir, DirName, StateFileName)
}

// LoadState reads durable state; missing file → zero state.
func LoadState(stateDir string) (State, error) {
	var st State
	data, err := os.ReadFile(StatePath(stateDir))
	if os.IsNotExist(err) {
		return st, nil
	}
	if err != nil {
		return st, fmt.Errorf("audit state: read: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return st, nil
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return st, fmt.Errorf("audit state: parse: %w", err)
	}
	return st, nil
}

// SaveState atomically writes state.
func SaveState(stateDir string, st State) error {
	dir := filepath.Join(stateDir, DirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("audit state: mkdir: %w", err)
	}
	return writeJSONAtomic(StatePath(stateDir), st)
}

// AllowCycle applies the cost guard: cycles within the trailing 24h must
// stay under MaxCyclesPerDay, and consecutive cycles must be at least
// MinCycleGap apart. Manual reasons bypass the daily cap only when the
// caller passes force. Returns the reason for refusal when not allowed.
func AllowCycle(cfg Config, st State, now time.Time, force bool) (bool, string) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if !force {
		if gap := cfg.MinCycleGap(); gap > 0 && !st.LastCycleAt.IsZero() {
			if since := now.Sub(st.LastCycleAt); since < gap {
				return false, fmt.Sprintf("min cycle gap %s not elapsed (%s since last)", gap, since.Round(time.Second))
			}
		}
		max := cfg.EffectiveMaxCyclesPerDay()
		if max > 0 && len(pruneWindow(st.RecentCycles, now, 24*time.Hour)) >= max {
			return false, fmt.Sprintf("cost guard: %d cycles already run in the last 24h", max)
		}
	}
	return true, ""
}

// recordCycle stamps a completed cycle into the rolling window.
func recordCycle(st State, now time.Time) State {
	st.LastCycleAt = now
	st.CycleCount++
	st.RecentCycles = append(pruneWindow(st.RecentCycles, now, 24*time.Hour), now)
	return st
}

func pruneWindow(stamps []time.Time, now time.Time, window time.Duration) []time.Time {
	out := make([]time.Time, 0, len(stamps))
	cutoff := now.Add(-window)
	for _, t := range stamps {
		if t.After(cutoff) {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}

// writeJSONAtomic marshals v and writes it write-and-rename.
func writeJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("audit: marshal %s: %w", path, err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("audit: mkdir %s: %w", filepath.Dir(path), err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("audit: write tmp %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("audit: rename %s: %w", path, err)
	}
	return nil
}
