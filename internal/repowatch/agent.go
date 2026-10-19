// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package repowatch

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/jevons/internal/capacity"
)

// StateFileName records what the standing cycle last did.
const StateFileName = "state.json"

// firstDelay keeps boot cheap without postponing the first pass by a whole
// interval when the daemon restarts.
const firstDelay = 2 * time.Minute

// BriefDeliverer posts a repo-watch brief to a named PO (fire-and-forget).
type BriefDeliverer interface {
	DeliverRepoWatch(po, text string) error
}

// Args configures the standing repowatch agent (🎯T1022.2).
type Args struct {
	// StateDir holds config, per-repo watermarks, and run state (required).
	StateDir string
	// Runner executes gh. Nil → ExecRunner{}.
	Runner Runner
	// Seats supplies live PO workdirs. Nil → no auto-discovery (config only).
	Seats SeatLister
	// Deliverer posts briefs to POs. Nil means watermark-only (no delivery).
	Deliverer BriefDeliverer
	// Capacity admits or defers scheduled passes (🎯T359). Nil runs every
	// scheduled pass. Owner-invoked cycles (MCP, tests) are never gated.
	Capacity capacity.Gate
	// Interval overrides the config cadence when non-zero.
	Interval time.Duration
	// DryRun never delivers.
	DryRun bool
	// Now optional clock.
	Now func() time.Time
	// OnResult optional hook after each cycle.
	OnResult func(CycleResult)
}

// RepoResult is one repo's outcome in a cycle.
type RepoResult struct {
	Repo      string `json:"repo"`
	PO        string `json:"po,omitempty"`
	Workdir   string `json:"workdir,omitempty"`
	Fetched   int    `json:"fetched"`
	Fresh     int    `json:"fresh"`
	Delivered bool   `json:"delivered"`
	Skipped   string `json:"skipped,omitempty"`
	Error     string `json:"error,omitempty"`
	Brief     string `json:"brief,omitempty"`
}

// CycleResult is one bounded repowatch cycle.
type CycleResult struct {
	Trigger string       `json:"trigger"`
	Repos   []RepoResult `json:"repos"`
	Skipped []string     `json:"skipped,omitempty"`
}

// FreshCount is the number of new/updated items across repos.
func (r CycleResult) FreshCount() int {
	n := 0
	for _, rr := range r.Repos {
		n += rr.Fresh
	}
	return n
}

// State is the durable record of the standing cycle's activity.
type State struct {
	Runs           int    `json:"runs"`
	LastRunAt      string `json:"last_run_at,omitempty"`
	LastTrigger    string `json:"last_trigger,omitempty"`
	LastFresh      int    `json:"last_fresh,omitempty"`
	BriefsSent     int    `json:"briefs_sent,omitempty"`
	LastBriefAt    string `json:"last_brief_at,omitempty"`
	LastSkipReason string `json:"last_skip_reason,omitempty"`
}

// Agent is the standing repowatch staff cycle.
type Agent struct {
	args Args
	mu   sync.Mutex
}

// New validates args.
func New(args Args) (*Agent, error) {
	if strings.TrimSpace(args.StateDir) == "" {
		return nil, fmt.Errorf("repowatch: StateDir required")
	}
	if args.Now == nil {
		args.Now = func() time.Time { return time.Now().UTC() }
	}
	if args.Runner == nil {
		args.Runner = ExecRunner{}
	}
	// Ensure the state subtree exists.
	if err := os.MkdirAll(filepath.Join(expandHome(args.StateDir), DirName), 0o755); err != nil {
		return nil, fmt.Errorf("repowatch: mkdir: %w", err)
	}
	return &Agent{args: args}, nil
}

// StateDir returns the configured state directory.
func (a *Agent) StateDir() string {
	if a == nil {
		return ""
	}
	return a.args.StateDir
}

// Config reloads the durable config.
func (a *Agent) Config() (Config, error) {
	if a == nil {
		return Config{}, fmt.Errorf("repowatch: nil agent")
	}
	return LoadConfig(a.args.StateDir)
}

// PatchConfig applies an overseer retune and persists it.
func (a *Agent) PatchConfig(patch ConfigPatch) (Config, error) {
	if a == nil {
		return Config{}, fmt.Errorf("repowatch: nil agent")
	}
	if patch.Now.IsZero() {
		patch.Now = a.args.Now()
	}
	return PatchConfig(a.args.StateDir, patch)
}

// State returns the durable run record.
func (a *Agent) State() (State, error) {
	if a == nil {
		return State{}, fmt.Errorf("repowatch: nil agent")
	}
	return LoadState(a.args.StateDir)
}

// LoadState reads the durable run record.
func LoadState(stateDir string) (State, error) {
	path := filepath.Join(expandHome(stateDir), DirName, StateFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return State{}, nil
		}
		return State{}, fmt.Errorf("repowatch state: read %s: %w", path, err)
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return State{}, fmt.Errorf("repowatch state: parse %s: %w", path, err)
	}
	return st, nil
}

// SaveState persists the run record atomically.
func SaveState(stateDir string, st State) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("repowatch state: marshal: %w", err)
	}
	return writeFileAtomic(filepath.Join(expandHome(stateDir), DirName, StateFileName), append(data, '\n'), 0o644)
}

// Run starts the standing schedule until ctx is done.
func (a *Agent) Run(ctx context.Context) {
	if a == nil {
		return
	}
	cfg, err := LoadConfig(a.args.StateDir)
	if err != nil {
		slog.Warn("repowatch schedule disabled", "err", err)
		<-ctx.Done()
		return
	}
	interval := cfg.IntervalDuration()
	if a.args.Interval != 0 {
		interval = a.args.Interval
	}
	if interval <= 0 {
		<-ctx.Done()
		return
	}
	delay := min(firstDelay, interval)
	first := time.NewTimer(delay)
	defer first.Stop()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-first.C:
			a.runSafe("schedule_boot")
		case <-ticker.C:
			next, err := LoadConfig(a.args.StateDir)
			if err != nil {
				continue
			}
			if !next.Enabled {
				continue
			}
			if d := next.IntervalDuration(); d > 0 && d != interval && a.args.Interval == 0 {
				ticker.Stop()
				interval = d
				ticker = time.NewTicker(interval)
			}
			a.runSafe("schedule")
		}
	}
}

func (a *Agent) runSafe(reason string) {
	// Ambient class: same bucket as research (poll-shaped awareness).
	verdict, release := capacity.Ask(a.args.Capacity, capacity.ClassResearch, "repowatch."+reason)
	defer release()
	if !verdict.Admitted() {
		a.noteCapacitySkip(reason, verdict)
		return
	}
	res, err := a.RunOnce(context.Background(), reason)
	if err != nil {
		slog.Warn("repowatch cycle failed", "reason", reason, "err", err)
		return
	}
	if res.FreshCount() > 0 {
		slog.Info("repowatch cycle",
			"reason", reason,
			"repos", len(res.Repos),
			"fresh", res.FreshCount(),
		)
	}
}

func (a *Agent) noteCapacitySkip(reason string, d capacity.Decision) {
	slog.Info("repowatch cycle deferred by capacity",
		"reason", reason, "verdict", d.Verdict, "cause", d.Reason,
		"pressure", d.Pressure.String(), "detail", d.Detail)
	st, _ := LoadState(a.args.StateDir)
	st.LastSkipReason = fmt.Sprintf("capacity %s: %s (%s)", d.Verdict, d.Reason, d.Detail)
	_ = SaveState(a.args.StateDir, st)
}

// RunOnce executes one poll cycle now (schedule, MCP, or tests).
func (a *Agent) RunOnce(ctx context.Context, reason string) (CycleResult, error) {
	if a == nil {
		return CycleResult{}, fmt.Errorf("repowatch: nil agent")
	}
	cfg, err := LoadConfig(a.args.StateDir)
	if err != nil {
		return CycleResult{}, err
	}
	manual := reason == "mcp" || reason == "test"
	if !cfg.Enabled && !manual {
		return CycleResult{Trigger: reason, Skipped: []string{"disabled"}}, nil
	}
	now := a.args.Now()
	var seats []Seat
	if a.args.Seats != nil {
		seats = a.args.Seats.List()
	}
	targets := DiscoverTargets(cfg, seats)
	if len(targets) == 0 {
		res := CycleResult{Trigger: reason, Skipped: []string{"no watch targets (no PO seats / no config repos)"}}
		a.finish(res, now)
		return res, nil
	}

	limit := cfg.EffectiveMaxItemsPerRepo()
	res := CycleResult{Trigger: reason}
	for _, t := range targets {
		rr := a.pollOne(ctx, cfg, t, limit, reason, now)
		res.Repos = append(res.Repos, rr)
	}
	a.finish(res, now)
	return res, nil
}

func (a *Agent) pollOne(ctx context.Context, cfg Config, t WatchTarget, limit int, reason string, now time.Time) RepoResult {
	rr := RepoResult{PO: t.PO, Workdir: t.Workdir, Repo: t.Repo}
	if !t.Enabled {
		rr.Skipped = "disabled"
		return rr
	}
	if t.Workdir == "" {
		rr.Skipped = "no workdir"
		return rr
	}
	repo := t.Repo
	if repo == "" {
		resolved, err := ResolveRepo(ctx, a.args.Runner, t.Workdir)
		if err != nil {
			rr.Error = "resolve repo: " + err.Error()
			return rr
		}
		repo = resolved
		rr.Repo = repo
	}
	if repo == "" {
		rr.Skipped = "empty repo"
		return rr
	}
	// Per-repo opt-out via config even when discovered from a seat.
	if rc, ok := cfg.RepoByName(repo); ok && !rc.EnabledOrDefault() {
		rr.Skipped = "disabled by config"
		return rr
	}

	st, err := LoadRepoState(a.args.StateDir, repo)
	if err != nil {
		rr.Error = err.Error()
		return rr
	}
	st.PO = t.PO
	st.Workdir = t.Workdir
	st.Repo = repo

	issues, err := ListIssues(ctx, a.args.Runner, t.Workdir, limit)
	if err != nil {
		rr.Error = "issues: " + err.Error()
		st.LastError = rr.Error
		st.LastPoll = now.UTC().Format(time.RFC3339)
		st.Polls++
		_ = SaveRepoState(a.args.StateDir, st)
		return rr
	}
	prs, err := ListPRs(ctx, a.args.Runner, t.Workdir, limit)
	if err != nil {
		rr.Error = "prs: " + err.Error()
		st.LastError = rr.Error
		st.LastPoll = now.UTC().Format(time.RFC3339)
		st.Polls++
		_ = SaveRepoState(a.args.StateDir, st)
		return rr
	}
	all := append(issues, prs...)
	rr.Fetched = len(all)
	fresh, next := Delta(st, all)
	rr.Fresh = len(fresh)
	next.LastPoll = now.UTC().Format(time.RFC3339)
	next.LastError = ""
	next.Polls++
	next.PO = t.PO
	next.Workdir = t.Workdir

	if len(fresh) == 0 {
		_ = SaveRepoState(a.args.StateDir, next)
		return rr
	}

	brief := FormatBrief(Brief{
		Repo:    repo,
		PO:      t.PO,
		At:      now,
		Items:   fresh,
		Trigger: reason,
	})
	rr.Brief = brief

	if !a.args.DryRun && a.args.Deliverer != nil && t.PO != "" {
		if err := a.args.Deliverer.DeliverRepoWatch(t.PO, brief); err != nil {
			rr.Error = "deliver: " + err.Error()
			// Still advance the watermark: a flapping deliverer must not
			// re-spam the same items forever. The error is visible on the
			// repo state and cycle result.
			next.LastError = rr.Error
		} else {
			rr.Delivered = true
			next.Delivered++
		}
	}
	if err := SaveRepoState(a.args.StateDir, next); err != nil {
		rr.Error = "save state: " + err.Error()
	}
	return rr
}

func (a *Agent) finish(res CycleResult, now time.Time) {
	st, _ := LoadState(a.args.StateDir)
	st.Runs++
	st.LastRunAt = now.UTC().Format(time.RFC3339)
	st.LastTrigger = res.Trigger
	st.LastFresh = res.FreshCount()
	delivered := 0
	for _, rr := range res.Repos {
		if rr.Delivered {
			delivered++
		}
	}
	if delivered > 0 {
		st.BriefsSent += delivered
		st.LastBriefAt = now.UTC().Format(time.RFC3339)
	}
	if len(res.Skipped) > 0 {
		st.LastSkipReason = res.Skipped[len(res.Skipped)-1]
	} else {
		st.LastSkipReason = ""
	}
	_ = SaveState(a.args.StateDir, st)
	if a.args.OnResult != nil {
		a.args.OnResult(res)
	}
}

// StatusSnapshot is the inspectable surface for MCP + GET /api/repowatch.
type StatusSnapshot struct {
	Config Config      `json:"config"`
	State  State       `json:"state"`
	Repos  []RepoState `json:"repos"`
}

// Snapshot returns config + run state + per-repo watermarks.
func (a *Agent) Snapshot() (StatusSnapshot, error) {
	if a == nil {
		return StatusSnapshot{}, fmt.Errorf("repowatch: nil agent")
	}
	cfg, err := LoadConfig(a.args.StateDir)
	if err != nil {
		return StatusSnapshot{}, err
	}
	st, err := LoadState(a.args.StateDir)
	if err != nil {
		return StatusSnapshot{}, err
	}
	repos, err := ListRepoStates(a.args.StateDir)
	if err != nil {
		return StatusSnapshot{}, err
	}
	return StatusSnapshot{Config: cfg, State: st, Repos: repos}, nil
}
