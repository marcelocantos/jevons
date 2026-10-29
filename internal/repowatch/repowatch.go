// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package repowatch implements the 🎯T1022.2 standing cycle: poll `gh issue
// list` / `gh pr list` per PO workdir at a bounded, capacity-gated cadence,
// track a watermark per repo, and deliver new/updated items to the owning PO
// as a typed repo-watch brief. The daemon owns awareness; the PO owns
// judgment (🎯T1022.3 triage doctrine); escalation transport is a separate
// package.
//
// Shape mirrors internal/research and internal/audit: durable config,
// durable per-repo state, a capacity-gated ticker, hermetic tests against a
// fake `gh` runner — no live GitHub calls in CI.
package repowatch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/jevons/internal/capacity"
)

// DefaultInterval matches the research feed cadence (🎯T1022.2 acceptance:
// "default: research-feed interval, 30 min").
const DefaultInterval = 30 * time.Minute

// DirName is the repowatch subtree under state_dir.
const DirName = "repowatch"

// ConfigFileName is the durable, overseer-retunable config.
const ConfigFileName = "config.json"

// EventSource labels repo-watch briefs on the wire to a PO.
const EventSource = "repo-watch"

// firstDelay keeps boot cheap without postponing the first pass by a whole
// interval when the daemon restarts.
const firstDelay = 2 * time.Minute

// RepoSpec names one repository a PO watches.
type RepoSpec struct {
	// Name is the repo's short name, used as the state-file key
	// (state_dir/repowatch/<repo>.json) and in briefs.
	Name string
	// Dir is the working directory `gh` runs in (must be a clone of the repo).
	Dir string
	// Owner is the fleet agent name the brief is delivered to (the PO).
	Owner string
}

// Deliverer posts a repo-watch brief to a named agent (fire-and-forget).
type Deliverer interface {
	Deliver(target, text string) error
}

// Config is the durable, overseer-retunable repowatch configuration.
type Config struct {
	Enabled     bool `json:"enabled"`
	IntervalSec int  `json:"interval_sec"`
	Deliver     bool `json:"deliver"`
	// DisabledRepos opts a repo out of the watch by name (owner ask:
	// non-marcelocantos orgs default to watched; this is the opt-out knob).
	DisabledRepos []string `json:"disabled_repos,omitempty"`

	UpdatedAt string `json:"updated_at,omitempty"`
	UpdatedBy string `json:"updated_by,omitempty"`
}

// DefaultConfig is the shipped posture: watching and delivering, on the
// research-feed cadence.
func DefaultConfig() Config {
	return Config{
		Enabled:     true,
		IntervalSec: int(DefaultInterval / time.Second),
		Deliver:     true,
	}
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			if p == "~" {
				return home
			}
			return filepath.Join(home, p[2:])
		}
	}
	return p
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
		return Config{}, fmt.Errorf("repowatch config: parse %s: %w", path, err)
	}
	if cfg.IntervalSec == 0 {
		cfg.IntervalSec = int(DefaultInterval / time.Second)
	}
	return cfg, nil
}

// SaveConfig writes durable config atomically.
func SaveConfig(stateDir string, cfg Config) error {
	path := ConfigPath(stateDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("repowatch config: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("repowatch config: marshal: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("repowatch config: write: %w", err)
	}
	return os.Rename(tmp, path)
}

// IntervalOf returns the configured cadence as a Duration.
func (c Config) IntervalOf() time.Duration {
	if c.IntervalSec <= 0 {
		return DefaultInterval
	}
	return time.Duration(c.IntervalSec) * time.Second
}

// RepoDisabled reports whether a named repo has been opted out.
func (c Config) RepoDisabled(name string) bool {
	for _, d := range c.DisabledRepos {
		if strings.EqualFold(strings.TrimSpace(d), strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

// ConfigPatch holds nil-able overrides for Configure (🎯T1022.2 overseer
// retune surface), mirroring internal/research's patch shape.
type ConfigPatch struct {
	Enabled       *bool
	IntervalSec   *int
	Deliver       *bool
	AddDisabled   string
	RemoveEnabled string
	UpdatedBy     string
}

// Apply returns cfg with the patch applied.
func (p ConfigPatch) Apply(cfg Config) Config {
	if p.Enabled != nil {
		cfg.Enabled = *p.Enabled
	}
	if p.IntervalSec != nil {
		cfg.IntervalSec = *p.IntervalSec
	}
	if p.Deliver != nil {
		cfg.Deliver = *p.Deliver
	}
	if name := strings.TrimSpace(p.AddDisabled); name != "" && !cfg.RepoDisabled(name) {
		cfg.DisabledRepos = append(cfg.DisabledRepos, name)
	}
	if name := strings.TrimSpace(p.RemoveEnabled); name != "" {
		out := make([]string, 0, len(cfg.DisabledRepos))
		for _, d := range cfg.DisabledRepos {
			if !strings.EqualFold(strings.TrimSpace(d), name) {
				out = append(out, d)
			}
		}
		cfg.DisabledRepos = out
	}
	if strings.TrimSpace(p.UpdatedBy) != "" {
		cfg.UpdatedBy = p.UpdatedBy
	}
	cfg.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return cfg
}

// Item is one GitHub issue or pull request as seen by `gh ... list --json`.
type Item struct {
	Kind      string `json:"kind"` // "issue" | "pr"
	Number    int    `json:"number"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	Author    string `json:"author"`
	State     string `json:"state"`
	UpdatedAt string `json:"updated_at"`
	Draft     bool   `json:"draft,omitempty"`
	Additions int    `json:"additions,omitempty"`
	Deletions int    `json:"deletions,omitempty"`
	Files     int    `json:"changed_files,omitempty"`
	CIState   string `json:"ci_state,omitempty"`
}

// UpdatedTime parses UpdatedAt, zero time on failure.
func (it Item) UpdatedTime() time.Time {
	t, err := time.Parse(time.RFC3339, it.UpdatedAt)
	if err != nil {
		return time.Time{}
	}
	return t
}

// Key identifies an item uniquely within a repo, for dedup/sort.
func (it Item) Key() string {
	return fmt.Sprintf("%s#%d", it.Kind, it.Number)
}

// GhRunner executes a `gh` subcommand in a working directory and returns its
// stdout. Hermetic tests inject a fake; production uses ExecRunner.
type GhRunner interface {
	Run(ctx context.Context, dir string, args ...string) ([]byte, error)
}

// RepoState is the durable per-repo watermark (state_dir/repowatch/<repo>.json).
type RepoState struct {
	Repo      string    `json:"repo"`
	Watermark time.Time `json:"watermark"`
	CheckedAt string    `json:"checked_at,omitempty"`
	LastItems []Item    `json:"last_items,omitempty"`
}

func statePath(stateDir, repo string) string {
	return filepath.Join(expandHome(stateDir), DirName, safeFile(repo)+".json")
}

func safeFile(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// LoadState reads the durable watermark for one repo; zero state (watermark
// at the zero time, which scans as "everything is new") when absent.
func LoadState(stateDir, repo string) (RepoState, error) {
	path := statePath(stateDir, repo)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return RepoState{Repo: repo}, nil
		}
		return RepoState{}, fmt.Errorf("repowatch state: read %s: %w", path, err)
	}
	var st RepoState
	if err := json.Unmarshal(data, &st); err != nil {
		return RepoState{}, fmt.Errorf("repowatch state: parse %s: %w", path, err)
	}
	return st, nil
}

// SaveState writes the durable watermark for one repo atomically.
func SaveState(stateDir string, st RepoState) error {
	path := statePath(stateDir, st.Repo)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("repowatch state: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("repowatch state: marshal: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("repowatch state: write: %w", err)
	}
	return os.Rename(tmp, path)
}

// Args configures the standing repowatch agent.
type Args struct {
	// StateDir holds config and per-repo watermark state (required).
	StateDir string
	// Repos are the PO-owned repositories to watch. Discovery (from the
	// fleet registry) is the daemon-wiring caller's job, not this package's.
	Repos []RepoSpec
	// Runner executes `gh` (default: ExecRunner, the real `gh` binary).
	Runner GhRunner
	// Deliverer posts briefs to a PO (nil means state-only, no delivery).
	Deliverer Deliverer
	// Capacity admits or defers scheduled passes (🎯T359). Nil admits every
	// scheduled pass. Owner-invoked cycles (MCP) are never gated.
	Capacity capacity.Gate
	// Interval overrides the config cadence when non-zero. Negative disables
	// the ticker (tools stay registered).
	Interval time.Duration
	// Now is an optional clock (tests).
	Now func() time.Time
}

// Agent is the standing repowatch cycle.
type Agent struct {
	mu   sync.Mutex
	args Args
	now  func() time.Time
}

// New builds a repowatch Agent. StateDir is required; everything else has a
// safe default.
func New(args Args) (*Agent, error) {
	if strings.TrimSpace(args.StateDir) == "" {
		return nil, fmt.Errorf("repowatch: StateDir required")
	}
	if args.Runner == nil {
		args.Runner = ExecRunner{}
	}
	if args.Now == nil {
		args.Now = time.Now
	}
	return &Agent{args: args, now: args.Now}, nil
}

// Config reads the agent's durable config.
func (a *Agent) Config() (Config, error) {
	return LoadConfig(a.args.StateDir)
}

// Repos returns the configured repo specs (copy).
func (a *Agent) Repos() []RepoSpec {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]RepoSpec, len(a.args.Repos))
	copy(out, a.args.Repos)
	return out
}

// RepoCycleResult is one repo's outcome within a cycle.
type RepoCycleResult struct {
	Repo      string    `json:"repo"`
	New       []Item    `json:"new,omitempty"`
	Watermark time.Time `json:"watermark"`
	Delivered bool      `json:"delivered"`
	Skipped   string    `json:"skipped,omitempty"`
}

// CycleResult is one bounded repowatch cycle across every watched repo.
type CycleResult struct {
	Trigger string            `json:"trigger"`
	Repos   []RepoCycleResult `json:"repos"`
	Skipped []string          `json:"skipped,omitempty"`
}

// Changed reports whether the cycle found anything new.
func (r CycleResult) Changed() bool {
	for _, rc := range r.Repos {
		if len(rc.New) > 0 {
			return true
		}
	}
	return false
}

// RunOnce runs one bounded cycle across every configured repo: `gh issue
// list` and `gh pr list` since each repo's watermark, delivering a brief for
// any repo with new/updated items, persisting the advanced watermark either
// way (so a transient delivery failure does not replay forever — a brief
// that failed to deliver is reported in Skipped, not silently dropped).
func (a *Agent) RunOnce(trigger string) (CycleResult, error) {
	cfg, err := a.Config()
	if err != nil {
		return CycleResult{}, err
	}
	res := CycleResult{Trigger: trigger}
	if !cfg.Enabled {
		res.Skipped = append(res.Skipped, "repowatch disabled")
		return res, nil
	}
	for _, repo := range a.Repos() {
		if cfg.RepoDisabled(repo.Name) {
			res.Repos = append(res.Repos, RepoCycleResult{Repo: repo.Name, Skipped: "opted out"})
			continue
		}
		rc, err := a.scanRepo(repo, cfg)
		if err != nil {
			rc.Skipped = fmt.Sprintf("scan error: %v", err)
		}
		res.Repos = append(res.Repos, rc)
	}
	return res, nil
}

func (a *Agent) scanRepo(repo RepoSpec, cfg Config) (RepoCycleResult, error) {
	ctx := context.Background()
	st, err := LoadState(a.args.StateDir, repo.Name)
	if err != nil {
		return RepoCycleResult{Repo: repo.Name}, err
	}
	issues, err := ListIssues(ctx, a.args.Runner, repo.Dir)
	if err != nil {
		return RepoCycleResult{Repo: repo.Name, Watermark: st.Watermark}, fmt.Errorf("issue list: %w", err)
	}
	prs, err := ListPRs(ctx, a.args.Runner, repo.Dir)
	if err != nil {
		return RepoCycleResult{Repo: repo.Name, Watermark: st.Watermark}, fmt.Errorf("pr list: %w", err)
	}
	all := append(issues, prs...)
	sort.Slice(all, func(i, j int) bool { return all[i].UpdatedTime().Before(all[j].UpdatedTime()) })

	var fresh []Item
	newWatermark := st.Watermark
	for _, it := range all {
		t := it.UpdatedTime()
		if t.After(st.Watermark) {
			fresh = append(fresh, it)
		}
		if t.After(newWatermark) {
			newWatermark = t
		}
	}

	rc := RepoCycleResult{Repo: repo.Name, New: fresh, Watermark: newWatermark}
	if len(fresh) > 0 && cfg.Deliver && a.args.Deliverer != nil && strings.TrimSpace(repo.Owner) != "" {
		brief := RenderBrief(repo.Name, fresh)
		if err := a.args.Deliverer.Deliver(repo.Owner, brief); err != nil {
			rc.Skipped = fmt.Sprintf("deliver failed: %v", err)
		} else {
			rc.Delivered = true
		}
	}
	st.Watermark = newWatermark
	st.CheckedAt = a.now().UTC().Format(time.RFC3339)
	st.LastItems = fresh
	if err := SaveState(a.args.StateDir, st); err != nil {
		return rc, fmt.Errorf("save state: %w", err)
	}
	return rc, nil
}

// Run drives the standing ticker until ctx is cancelled. Each tick is
// capacity-gated (🎯T359); a deferred tick is silently skipped — the next
// tick tries again.
func (a *Agent) Run(ctx context.Context) {
	cfg, _ := a.Config()
	interval := a.args.Interval
	if interval == 0 {
		interval = cfg.IntervalOf()
	}
	if interval < 0 {
		return
	}
	delay := firstDelay
	if delay > interval {
		delay = interval
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			d, release := capacity.Ask(a.args.Capacity, capacity.ClassResearch, "repowatch")
			if d.Admitted() {
				_, _ = a.RunOnce("schedule")
			}
			release()
			cfg, _ := a.Config()
			next := a.args.Interval
			if next == 0 {
				next = cfg.IntervalOf()
			}
			if next < 0 {
				return
			}
			timer.Reset(next)
		}
	}
}

// StateDir returns the agent's configured state directory (for callers that
// need to read per-repo state directly, e.g. the MCP status surface).
func (a *Agent) StateDir() string {
	return a.args.StateDir
}

// ListStates returns every persisted per-repo watermark state under
// state_dir/repowatch, sorted by repo name. Used by the read-only HTTP/API
// surface, which has no live Agent reference.
func ListStates(stateDir string) ([]RepoState, error) {
	dir := filepath.Join(expandHome(stateDir), DirName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("repowatch: list states: %w", err)
	}
	var out []RepoState
	for _, e := range entries {
		if e.IsDir() || e.Name() == ConfigFileName || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var st RepoState
		if err := json.Unmarshal(data, &st); err != nil {
			continue
		}
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Repo < out[j].Repo })
	return out, nil
}
