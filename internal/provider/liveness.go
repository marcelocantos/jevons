// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/jevons/internal/config"
)

// Automation liveness (🎯T27.9): jevonsd tracks declared automations
// (config.yaml `automations:`) and surfaces a stall — a signal source
// silent past cadence×grace — in the aggregated provider model, plus a
// notification on every stall/recovery transition. Signal sources are
// generic probers composed declaratively per automation (file mtime,
// newest artifact, git last-commit, launchd LastExitStatus, aggregated
// provider feed); adding an automation is config-only. Reborn from the
// former mnemo-T98 nine-day silent ytt stall.

// Automation liveness states.
const (
	// AutomationOK: the signal is fresh (within cadence×grace).
	AutomationOK = "ok"
	// AutomationStalled: the signal is silent past deadline, or the
	// source reports the automation unhealthy (launchd exit ≠ 0).
	AutomationStalled = "stalled"
	// AutomationUnknown: the source could not be probed (error).
	AutomationUnknown = "unknown"
)

// LivenessProviderID is the synthetic provider id under which liveness
// folds into the aggregated model; LivenessFeed is its single feed.
const (
	LivenessProviderID = "liveness"
	LivenessFeed       = "automations"
)

// DefaultLivenessInterval is the production check cadence. Stalls are
// detected within one interval of their deadline.
const DefaultLivenessInterval = time.Minute

// AutomationStatus is the owner-visible state of one tracked automation.
type AutomationStatus struct {
	ID    string `json:"id"`
	State string `json:"state"` // ok | stalled | unknown
	// Detail explains a stalled/unknown state (e.g. "no signal for 216h",
	// "last exit status 78", probe error).
	Detail string `json:"detail,omitempty"`
	// LastSignal is the newest signal time the source reported (zero when
	// the source never signalled or reports health, not time).
	LastSignal time.Time `json:"last_signal,omitzero"`
	// Deadline is when the automation stalls without a fresh signal
	// (LastSignal + cadence×grace); zero for health-only sources.
	Deadline  time.Time     `json:"deadline,omitzero"`
	Cadence   time.Duration `json:"cadence"`
	Grace     float64       `json:"grace"`
	Source    string        `json:"source"`
	CheckedAt time.Time     `json:"checked_at"`
}

// Signal is one probe result: when the automation last showed life, or —
// for health-style sources (launchd) — whether it is currently healthy.
type Signal struct {
	// At is the last-signal time. Zero means the source reports health
	// only (Healthy carries the verdict) or has never signalled.
	At time.Time
	// Healthy false forces a stall regardless of time (e.g. launchd job
	// missing or last exit non-zero).
	Healthy bool
	Detail  string
}

// RunCmd executes an external probe command and returns its combined
// stdout. The seam exists so launchd/git probes stay hermetically
// testable; nil uses exec.CommandContext.
type RunCmd func(ctx context.Context, name string, args ...string) (string, error)

// LivenessMonitor evaluates tracked automations on an interval, folds
// state transitions into the aggregated model as liveness feed events,
// and reports stall/recovery transitions to the notification sink.
type LivenessMonitor struct {
	decls    []config.AutomationDecl
	registry *Registry
	now      func() time.Time
	interval time.Duration
	runCmd   RunCmd
	onEvent  func(ev FeedEvent)
	onNotice func(st AutomationStatus)
	log      *slog.Logger

	mu     sync.Mutex
	states map[string]AutomationStatus
	seq    int64
}

// LivenessMonitorArgs configures a LivenessMonitor. Decls and Registry
// are required; everything else defaults to production behaviour.
type LivenessMonitorArgs struct {
	// Decls is the declared automation set (validated by config.Load).
	Decls []config.AutomationDecl
	// Registry receives folded liveness events (the aggregated model) and
	// serves provider-feed signal lookups. Required.
	Registry *Registry
	// OnEvent receives each folded liveness event — wire it to the
	// provider_event client broadcast. Nil means fold-only.
	OnEvent func(ev FeedEvent)
	// OnNotice fires on owner-worthy transitions: any → stalled, and
	// stalled → ok (recovery). Wire it to the overseer notify path.
	OnNotice func(st AutomationStatus)
	// Now is the clock (tests inject). Nil uses time.Now.
	Now func() time.Time
	// Interval is the check cadence. Zero uses DefaultLivenessInterval.
	Interval time.Duration
	// Run executes launchd/git probe commands. Nil uses the real command.
	Run RunCmd
	// Logger receives probe warnings. Nil uses slog.Default.
	Logger *slog.Logger
}

// NewLivenessMonitor returns a monitor ready to Run (or Check directly).
func NewLivenessMonitor(args LivenessMonitorArgs) *LivenessMonitor {
	if args.Registry == nil {
		panic("provider.NewLivenessMonitor: Registry is required")
	}
	m := &LivenessMonitor{
		decls:    args.Decls,
		registry: args.Registry,
		now:      args.Now,
		interval: args.Interval,
		runCmd:   args.Run,
		onEvent:  args.OnEvent,
		onNotice: args.OnNotice,
		log:      args.Logger,
		states:   make(map[string]AutomationStatus),
	}
	if m.now == nil {
		m.now = time.Now
	}
	if m.interval <= 0 {
		m.interval = DefaultLivenessInterval
	}
	if m.runCmd == nil {
		m.runCmd = func(ctx context.Context, name string, cmdArgs ...string) (string, error) {
			out, err := exec.CommandContext(ctx, name, cmdArgs...).CombinedOutput()
			return string(out), err
		}
	}
	if m.log == nil {
		m.log = slog.Default()
	}
	return m
}

// Run checks all automations once immediately, then on every interval
// tick until ctx is cancelled.
func (m *LivenessMonitor) Run(ctx context.Context) {
	m.Check(ctx)
	t := time.NewTicker(m.interval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			m.Check(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// Check evaluates every declared automation once. Exposed so tests (and
// future admin surfaces) can force a deterministic pass.
func (m *LivenessMonitor) Check(ctx context.Context) {
	for _, d := range m.decls {
		m.checkOne(ctx, d)
	}
}

// Statuses returns the current state of every tracked automation in
// declaration order (the owner-facing /api/automations view).
func (m *LivenessMonitor) Statuses() []AutomationStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]AutomationStatus, 0, len(m.decls))
	for _, d := range m.decls {
		if st, ok := m.states[d.ID]; ok {
			out = append(out, st)
		}
	}
	return out
}

// checkOne probes one automation, classifies it, and — on a state
// transition — folds a liveness event and notifies.
func (m *LivenessMonitor) checkOne(ctx context.Context, d config.AutomationDecl) {
	now := m.now()
	cadence, err := d.CadenceDuration()
	if err != nil {
		// Load validation makes this unreachable; degrade legibly anyway.
		m.transition(d, AutomationStatus{
			ID: d.ID, State: AutomationUnknown, Detail: err.Error(),
			Source: d.Source.Kind, CheckedAt: now,
		})
		return
	}
	grace := d.GraceMultiple()
	window := time.Duration(float64(cadence) * grace)

	st := AutomationStatus{
		ID: d.ID, Cadence: cadence, Grace: grace,
		Source: d.Source.Kind, CheckedAt: now,
	}
	sig, err := m.probe(ctx, d.Source)
	switch {
	case err != nil:
		st.State = AutomationUnknown
		st.Detail = err.Error()
		m.log.Warn("automation probe failed", "automation", d.ID, "kind", d.Source.Kind, "err", err)
	case !sig.Healthy:
		st.State = AutomationStalled
		st.Detail = sig.Detail
		st.LastSignal = sig.At
	case now.Sub(sig.At) > window:
		st.State = AutomationStalled
		st.LastSignal = sig.At
		st.Deadline = sig.At.Add(window)
		if sig.At.IsZero() {
			st.Detail = "no signal ever observed"
			st.Deadline = time.Time{}
		} else {
			st.Detail = fmt.Sprintf("no signal for %s (cadence %s × grace %g)",
				now.Sub(sig.At).Round(time.Second), cadence, grace)
		}
	default:
		st.State = AutomationOK
		st.LastSignal = sig.At
		st.Deadline = sig.At.Add(window)
	}
	m.transition(d, st)
}

// transition records the new status; when the state string changed it
// folds a liveness event into the aggregated model and fires the notice
// sink for owner-worthy transitions (→ stalled, stalled → ok).
func (m *LivenessMonitor) transition(d config.AutomationDecl, st AutomationStatus) {
	m.mu.Lock()
	prev, seen := m.states[d.ID]
	m.states[d.ID] = st
	changed := !seen || prev.State != st.State
	var seq int64
	if changed {
		m.seq++
		seq = m.seq
	}
	m.mu.Unlock()
	if !changed {
		return
	}

	kind := "status"
	switch {
	case st.State == AutomationStalled:
		kind = "stall"
	case st.State == AutomationOK && seen && prev.State == AutomationStalled:
		kind = "clear"
	}
	ev := FeedEvent{
		Feed: LivenessFeed, Seq: seq, TS: st.CheckedAt,
		Origin: LivenessProviderID, Kind: kind,
		Data: map[string]any{
			"automation": st.ID,
			"state":      st.State,
			"detail":     st.Detail,
			"source":     st.Source,
		},
	}
	if !st.LastSignal.IsZero() {
		ev.Data["last_signal"] = st.LastSignal.UTC().Format(time.RFC3339)
	}
	if m.registry.Ingest(LivenessProviderID, ev) && m.onEvent != nil {
		m.onEvent(ev)
	}
	m.log.Info("automation liveness transition",
		"automation", st.ID, "state", st.State, "detail", st.Detail)

	if m.onNotice != nil && (kind == "stall" || kind == "clear") {
		m.onNotice(st)
	}
}

// probe resolves one declared signal source to a Signal. Every branch is
// generic — an automation entry only composes these kinds with params.
func (m *LivenessMonitor) probe(ctx context.Context, src config.AutomationSource) (Signal, error) {
	switch src.Kind {
	case config.AutomationSourceFileMtime:
		path, err := expandHome(src.Path)
		if err != nil {
			return Signal{}, err
		}
		fi, err := os.Stat(path)
		if err != nil {
			return Signal{}, fmt.Errorf("file-mtime: %w", err)
		}
		return Signal{At: fi.ModTime(), Healthy: true}, nil

	case config.AutomationSourceNewestArtifact:
		dir, err := expandHome(src.Dir)
		if err != nil {
			return Signal{}, err
		}
		pattern := src.Glob
		if pattern == "" {
			pattern = "*"
		}
		matches, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			return Signal{}, fmt.Errorf("newest-artifact: bad glob %q: %w", pattern, err)
		}
		var newest time.Time
		for _, p := range matches {
			fi, err := os.Stat(p)
			if err != nil || fi.IsDir() {
				continue
			}
			if fi.ModTime().After(newest) {
				newest = fi.ModTime()
			}
		}
		// No artifacts is a valid probe: zero At reads as "never
		// signalled" and stalls once the window is exceeded.
		return Signal{At: newest, Healthy: true}, nil

	case config.AutomationSourceGitLastCommit:
		repo, err := expandHome(src.Repo)
		if err != nil {
			return Signal{}, err
		}
		out, err := m.runCmd(ctx, "git", "-C", repo, "log", "-1", "--format=%ct")
		if err != nil {
			return Signal{}, fmt.Errorf("git-last-commit: %w (%s)", err, strings.TrimSpace(out))
		}
		secs, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
		if err != nil {
			return Signal{}, fmt.Errorf("git-last-commit: parse %q: %w", strings.TrimSpace(out), err)
		}
		return Signal{At: time.Unix(secs, 0), Healthy: true}, nil

	case config.AutomationSourceLaunchd:
		// LastExitStatus is the signal: a loaded job whose last run exited
		// 0 is healthy; missing or failing is an immediate stall. launchd
		// keeps no last-run timestamp, so this source is health-style.
		out, err := m.runCmd(ctx, "launchctl", "list", src.Label)
		if err != nil {
			return Signal{Healthy: false, Detail: fmt.Sprintf("launchd job %s not loaded", src.Label)}, nil
		}
		mt := launchdExitRe.FindStringSubmatch(out)
		if mt == nil {
			// Loaded but never run (no LastExitStatus yet) — healthy.
			return Signal{At: m.now(), Healthy: true}, nil
		}
		if code, _ := strconv.Atoi(mt[1]); code != 0 {
			return Signal{Healthy: false, Detail: fmt.Sprintf("launchd job %s last exit status %d", src.Label, code)}, nil
		}
		return Signal{At: m.now(), Healthy: true}, nil

	case config.AutomationSourceProviderFeed:
		evs := m.registry.ModelFeed(src.Provider, src.Feed)
		if len(evs) == 0 {
			return Signal{Healthy: true}, nil // never signalled → stalls past window
		}
		return Signal{At: evs[len(evs)-1].TS, Healthy: true}, nil

	default:
		return Signal{}, fmt.Errorf("unknown signal source kind %q", src.Kind)
	}
}

// launchdExitRe extracts LastExitStatus from `launchctl list <label>`
// plist-style output: "LastExitStatus" = 0;
var launchdExitRe = regexp.MustCompile(`"LastExitStatus"\s*=\s*(-?\d+)`)

// expandHome resolves a leading ~ so config paths read naturally.
func expandHome(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand %q: %w", p, err)
		}
		return filepath.Join(home, strings.TrimPrefix(p, "~")), nil
	}
	return p, nil
}

// FormatAutomationNotice renders the owner notification for a stall or
// recovery transition.
func FormatAutomationNotice(st AutomationStatus) string {
	switch st.State {
	case AutomationStalled:
		detail := st.Detail
		if detail == "" {
			detail = "signal silent"
		}
		return fmt.Sprintf("⚠️ Automation stall: %s — %s (source %s).", st.ID, detail, st.Source)
	case AutomationOK:
		if st.LastSignal.IsZero() {
			return fmt.Sprintf("✅ Automation recovered: %s (source %s).", st.ID, st.Source)
		}
		return fmt.Sprintf("✅ Automation recovered: %s — fresh signal at %s (source %s).",
			st.ID, st.LastSignal.Local().Format(time.RFC3339), st.Source)
	default:
		return fmt.Sprintf("Automation %s state %s: %s", st.ID, st.State, st.Detail)
	}
}
