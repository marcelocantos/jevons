// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package audit

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/jevons/internal/capacity"
)

// Deliverer posts an audit notification to the overseer (fire-and-forget).
// The auditor never files bullseye targets itself: it suggests, the
// overseer disposes (same split as the RSI coach, 🎯T243).
type Deliverer interface {
	DeliverAudit(text string) error
}

// Args configures the auditor.
type Args struct {
	// StateDir holds config, state, reports, and residue (required).
	StateDir string
	// Workdir is the repository the auditor runs in.
	Workdir string
	// Home seeds default skills/prompt roots (usually $HOME).
	Home string
	// Runner dispatches a pass. Nil = dry cycles (manifest only).
	Runner Runner
	// Deliverer posts critical findings. Nil = no notification.
	Deliverer Deliverer
	// Capacity admits, degrades, or defers scheduled passes against the
	// fleet's remaining budget and concurrent load (🎯T359). A full-scan
	// audit on an advanced tier is one of the most expensive ambient
	// cycles, so it yields to owner turns and open Build work. Nil = no
	// admission control (every scheduled tick runs).
	Capacity capacity.Gate
	// Interval overrides config when non-zero at construction.
	Interval time.Duration
	// Ticks optionally replaces the schedule's real ticker, so a caller can
	// drive the cadence itself instead of waiting on wall clock (🎯T400).
	// A test asserting what the schedule *does* on a tick has no interest in
	// when the tick arrives, and pairing a short Interval with a polled
	// deadline makes it assert the machine is quiet as well: under load the
	// deferral test failed on that second assertion, not on the first. Nil =
	// real time, which is every production caller.
	Ticks <-chan time.Time
	// Now optional clock.
	Now func() time.Time
	// OnResult optional hook after each cycle.
	OnResult func(CycleResult)
}

// CycleResult is the outcome of one audit pass.
type CycleResult struct {
	Reason string `json:"reason,omitempty"`
	// Skipped is set with a reason when the cycle did not dispatch.
	Skipped string `json:"skipped,omitempty"`
	// Dry is set when no runner was configured: the manifest was built and
	// the pass stopped there.
	Dry bool `json:"dry,omitempty"`

	ReportID string   `json:"report_id,omitempty"`
	Manifest Manifest `json:"-"`
	Report   Report   `json:"-"`

	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	// Tier is the capacity tier the pass ran at (🎯T359): full, or reduced
	// when the fleet had headroom for a smaller pass but not a whole one.
	Tier string `json:"tier,omitempty"`

	FilesScanned  int         `json:"files_scanned"`
	CoveredScopes []ScopeKind `json:"covered_scopes,omitempty"`
	MissingScopes []ScopeKind `json:"missing_scopes,omitempty"`
	FullScan      bool        `json:"full_scan"`

	Merge    MergeResult   `json:"merge,omitzero"`
	Notified int           `json:"notified"`
	Duration time.Duration `json:"-"`
	Err      string        `json:"error,omitempty"`
}

// Auditor is the periodic full-scan audit cycle (🎯T357).
type Auditor struct {
	args    Args
	residue *ResidueStore
	mu      sync.Mutex
}

// New validates args and opens the residue ledger.
func New(args Args) (*Auditor, error) {
	if strings.TrimSpace(args.StateDir) == "" {
		return nil, fmt.Errorf("audit: StateDir required")
	}
	if args.Now == nil {
		args.Now = func() time.Time { return time.Now().UTC() }
	}
	if strings.TrimSpace(args.Home) == "" {
		args.Home, _ = os.UserHomeDir()
	}
	res, err := OpenResidueStore(args.StateDir)
	if err != nil {
		return nil, err
	}
	return &Auditor{args: args, residue: res}, nil
}

// StateDir returns the auditor state directory.
func (a *Auditor) StateDir() string {
	if a == nil {
		return ""
	}
	return a.args.StateDir
}

// Now returns the auditor's clock, so callers stamping durable records
// (residue dispositions, for one) share the cycle's notion of time.
func (a *Auditor) Now() time.Time {
	if a == nil || a.args.Now == nil {
		return time.Now().UTC()
	}
	return a.args.Now()
}

// Residue returns the durable finding ledger.
func (a *Auditor) Residue() *ResidueStore {
	if a == nil {
		return nil
	}
	return a.residue
}

// Config returns the durable, overseer-retunable config.
func (a *Auditor) Config() (Config, error) {
	if a == nil {
		return Config{}, fmt.Errorf("audit: nil auditor")
	}
	return LoadConfig(a.args.StateDir, a.args.Workdir, a.args.Home)
}

// PatchConfig applies an overseer retune and persists it.
func (a *Auditor) PatchConfig(patch ConfigPatch) (Config, error) {
	if a == nil {
		return Config{}, fmt.Errorf("audit: nil auditor")
	}
	if patch.Now.IsZero() {
		patch.Now = a.args.Now()
	}
	return PatchConfig(a.args.StateDir, a.args.Workdir, a.args.Home, patch)
}

// State returns durable cross-cycle bookkeeping.
func (a *Auditor) State() (State, error) {
	if a == nil {
		return State{}, fmt.Errorf("audit: nil auditor")
	}
	return LoadState(a.args.StateDir)
}

// Run starts the audit schedule until ctx is done. Interval changes made
// by an overseer retune are picked up on the next tick.
func (a *Auditor) Run(ctx context.Context) {
	if a == nil {
		return
	}
	cfg, _ := a.Config()
	interval := a.args.Interval
	if interval == 0 || cfg.UpdatedAt != "" {
		interval = cfg.IntervalDuration()
	}
	if interval <= 0 {
		<-ctx.Done()
		return
	}
	// A full scan at boot would collide with every other start-up cost, so
	// the first pass waits a settling delay rather than firing immediately.
	// An injected tick source supplies both, since a caller driving the
	// cadence is not waiting for the machine to settle either.
	var firstC, tickC <-chan time.Time
	var ticker *time.Ticker
	if a.args.Ticks != nil {
		tickC = a.args.Ticks
	} else {
		first := time.NewTimer(min(interval, 10*time.Minute))
		defer first.Stop()
		firstC = first.C
		ticker = time.NewTicker(interval)
		defer func() { ticker.Stop() }()
		tickC = ticker.C
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-firstC:
			if cfg, err := a.Config(); err == nil && cfg.Enabled {
				a.runSafe(ctx, "schedule_boot")
			}
		case <-tickC:
			next, err := a.Config()
			if err != nil {
				slog.Warn("audit config reload failed", "err", err)
				continue
			}
			if !next.Enabled {
				continue
			}
			if d := next.IntervalDuration(); ticker != nil && d > 0 && d != interval {
				ticker.Stop()
				interval = d
				ticker = time.NewTicker(interval)
				tickC = ticker.C
			}
			a.runSafe(ctx, "schedule")
		}
	}
}

// RunOnce executes one audit cycle immediately (MCP / tests) at full scope.
// Manual cycles bypass the daily cost guard when force is set. Scheduled
// passes go through runSafe, which may run at a reduced scope when capacity
// is elevated (🎯T359).
func (a *Auditor) RunOnce(ctx context.Context, reason string, force bool) (CycleResult, error) {
	if a == nil {
		return CycleResult{}, fmt.Errorf("audit: nil auditor")
	}
	return a.cycle(ctx, reason, force, capacity.TierFull)
}

// runSafe runs one scheduled pass subject to capacity admission (🎯T359).
// When the fleet is short of budget or busy with owner/Build work, this tick
// is skipped and the skip recorded durably — a full-scan audit is expensive
// and low-frequency, so a deferred tick costs nothing but the next cadence.
func (a *Auditor) runSafe(ctx context.Context, reason string) {
	verdict, release := capacity.Ask(a.args.Capacity, capacity.ClassAudit, reason)
	defer release()
	if !verdict.Admitted() {
		a.noteCapacitySkip(reason, verdict)
		return
	}
	res, err := a.cycle(ctx, reason, false, verdict.Tier)
	if err != nil {
		slog.Warn("audit cycle failed", "reason", reason, "err", err)
		return
	}
	if res.Skipped != "" {
		slog.Debug("audit cycle skipped", "reason", reason, "skipped", res.Skipped)
		return
	}
	slog.Info("audit cycle",
		"reason", reason,
		"report", res.ReportID,
		"model", res.Model,
		"files", res.FilesScanned,
		"full_scan", res.FullScan,
		"findings", len(res.Report.Findings),
		"new", len(res.Merge.New),
		"reopened", len(res.Merge.Reopened),
		"resolved", len(res.Merge.Resolved),
		"open_total", res.Merge.OpenTotal,
		"notified", res.Notified,
		"tier", res.Tier,
	)
}

// noteCapacitySkip records a deferred tick durably, so "why did the auditor
// go quiet?" is answerable from state.json rather than guessed at.
func (a *Auditor) noteCapacitySkip(reason string, d capacity.Decision) {
	slog.Info("audit cycle deferred by capacity",
		"reason", reason, "verdict", d.Verdict, "cause", d.Reason,
		"pressure", d.Pressure.String(), "detail", d.Detail)
	st, err := a.State()
	if err != nil {
		slog.Warn("audit state load failed", "err", err)
		return
	}
	st.LastSkipReason = fmt.Sprintf("capacity %s: %s (%s)", d.Verdict, d.Reason, d.Detail)
	if err := SaveState(a.args.StateDir, st); err != nil {
		slog.Warn("audit state save failed", "err", err)
	}
}

// reducedScope halves a bound for a degraded pass: half the manifest still
// audits every surface, at roughly half the tokens.
func reducedScope(n int) int {
	if n <= 1 {
		return n
	}
	return n / 2
}

// cycle runs one bounded audit pass end to end: scope → dispatch → parse →
// durable artifact → residue merge → in-cycle notify.
//
// tier is the capacity verdict's tier (🎯T359): capacity.TierReduced halves
// the manifest bounds and the finding cap so a pressured pass still covers
// every surface, just less of each, rather than being dropped outright.
func (a *Auditor) cycle(ctx context.Context, reason string, force bool, tier string) (CycleResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if tier == "" {
		tier = capacity.TierFull
	}
	res := CycleResult{Reason: reason, Tier: tier}
	start := a.args.Now()

	cfg, err := a.Config()
	if err != nil {
		return res, err
	}
	if tier == capacity.TierReduced {
		cfg.MaxFilesPerScope = reducedScope(cfg.Bounds().MaxFilesPerScope)
		cfg.MaxBytesPerScope = reducedScope(int(cfg.Bounds().MaxBytesPerScope))
		cfg.MaxFindings = reducedScope(cfg.EffectiveMaxFindings())
	}
	manual := force || reason == "mcp" || reason == "test"
	if !cfg.Enabled && !manual {
		res.Skipped = "disabled"
		return res, nil
	}
	st, err := a.State()
	if err != nil {
		return res, err
	}
	if ok, why := AllowCycle(cfg, st, start, force); !ok {
		res.Skipped = why
		return res, nil
	}

	man, err := BuildManifest(cfg, start)
	if err != nil {
		return res, err
	}
	res.Manifest = man
	res.FilesScanned = man.TotalFiles()
	res.CoveredScopes = man.CoveredScopes()
	res.MissingScopes = man.MissingScopes()
	res.FullScan = man.FullScan()
	res.Provider = cfg.EffectiveProvider()
	res.Model = cfg.EffectiveModel()

	if res.FilesScanned == 0 {
		res.Skipped = "no files in scope"
		return res, nil
	}
	if a.args.Runner == nil {
		// No auditor wired: the scope pass still ran and is reportable, but
		// nothing is merged — an empty report must never resolve residue.
		res.Dry = true
		res.Duration = a.args.Now().Sub(start)
		if a.args.OnResult != nil {
			a.args.OnResult(res)
		}
		return res, nil
	}

	maxFindings := cfg.EffectiveMaxFindings()
	assignment := Assignment{
		Manifest:       man,
		Prompt:         BuildPrompt(cfg, man, maxFindings),
		Provider:       res.Provider,
		Model:          res.Model,
		Workdir:        strings.TrimSpace(cfg.Workdir),
		Timeout:        cfg.Timeout(),
		MaxOutputBytes: cfg.EffectiveMaxOutputBytes(),
		Command:        cfg.EffectiveCommand(),
		Reason:         reason,
	}

	reportID := NewReportID(start)
	res.ReportID = reportID
	rep := Report{
		ID:            reportID,
		Reason:        reason,
		StartedAt:     start,
		Provider:      res.Provider,
		Model:         res.Model,
		Scopes:        res.CoveredScopes,
		MissingScopes: res.MissingScopes,
		Bounds:        man.Bounds,
		FilesInMan:    res.FilesScanned,
		Truncated:     man.Truncated(),
	}

	out, runErr := a.args.Runner.RunAudit(ctx, assignment)
	if out.Model != "" {
		rep.Model = out.Model
		res.Model = out.Model
	}
	if out.Provider != "" {
		rep.Provider = out.Provider
		res.Provider = out.Provider
	}
	if runErr != nil {
		// A failed pass is still durable evidence: the report records the
		// failure so a silently broken auditor cannot masquerade as
		// "nothing to report". Residue is left untouched.
		rep.Error = runErr.Error()
		rep.FinishedAt = a.args.Now()
		res.Report = rep
		res.Err = runErr.Error()
		_ = SaveReport(a.args.StateDir, rep, cfg.EffectiveKeepReports())
		a.saveState(st, res, start, runErr)
		return res, runErr
	}

	parsed, parseErr := ParseReport(out.Raw, ParseArgs{
		Workdir:     strings.TrimSpace(cfg.Workdir),
		MaxFindings: maxFindings,
	})
	if parseErr != nil {
		rep.Error = parseErr.Error()
		rep.FinishedAt = a.args.Now()
		res.Report = rep
		res.Err = parseErr.Error()
		_ = SaveReport(a.args.StateDir, rep, cfg.EffectiveKeepReports())
		a.saveState(st, res, start, parseErr)
		return res, parseErr
	}
	rep.Findings = parsed.Findings
	rep.Summary = parsed.Summary
	rep.Dropped = parsed.Dropped
	rep.FinishedAt = a.args.Now()
	res.Report = rep

	if err := SaveReport(a.args.StateDir, rep, cfg.EffectiveKeepReports()); err != nil {
		return res, err
	}

	merge, err := a.residue.Merge(MergeArgs{
		Report:         rep,
		CoveredScopes:  res.CoveredScopes,
		NotifySeverity: cfg.EffectiveNotifySeverity(),
		MaxNotify:      cfg.EffectiveMaxNotifies(),
		Now:            a.args.Now(),
	})
	if err != nil {
		return res, err
	}
	res.Merge = merge

	if cfg.Deliver && a.args.Deliverer != nil && len(merge.Notify) > 0 {
		text := FormatOverseerNotice(rep, merge, cfg)
		if err := a.args.Deliverer.DeliverAudit(text); err != nil {
			slog.Warn("audit notify failed", "err", err)
		} else {
			res.Notified = len(merge.Notify)
		}
	}

	res.Duration = a.args.Now().Sub(start)
	a.saveState(st, res, start, nil)
	if a.args.OnResult != nil {
		a.args.OnResult(res)
	}
	return res, nil
}

func (a *Auditor) saveState(st State, res CycleResult, start time.Time, cycleErr error) {
	st = recordCycle(st, start)
	st.LastReportID = res.ReportID
	st.LastReason = res.Reason
	st.LastFindings = len(res.Report.Findings)
	st.LastNotified = res.Notified
	st.LastModel = res.Model
	st.LastTier = res.Tier
	st.LastDurationS = a.args.Now().Sub(start).Seconds()
	if cycleErr != nil {
		st.LastError = cycleErr.Error()
	} else {
		st.LastError = ""
	}
	// A pass that actually dispatched clears any stale deferral note.
	st.LastSkipReason = ""
	if err := SaveState(a.args.StateDir, st); err != nil {
		slog.Warn("audit state save failed", "err", err)
	}
}

// FormatOverseerNotice builds the in-cycle wire text for the overseer.
// It leads with what changed, because a standing list of open findings is
// not news — new and reopened criticals are.
func FormatOverseerNotice(rep Report, merge MergeResult, cfg Config) string {
	var b strings.Builder
	b.WriteString("[code-auditor] full-scan audit ")
	b.WriteString(rep.ID)
	fmt.Fprintf(&b, " (%s", rep.Model)
	if len(rep.MissingScopes) > 0 {
		b.WriteString(", PARTIAL: no ")
		b.WriteString(joinScopes(rep.MissingScopes))
	}
	b.WriteString(")\n")
	fmt.Fprintf(&b, "findings: %d  new: %d  reopened: %d  resolved: %d  open total: %d\n",
		len(rep.Findings), len(merge.New), len(merge.Reopened), len(merge.Resolved), merge.OpenTotal)
	if rep.Truncated {
		b.WriteString("NOTE: scope was truncated by pass bounds — surfaces are not fully covered.\n")
	}

	threshold := cfg.EffectiveNotifySeverity()
	fmt.Fprintf(&b, "\nat or above %s:\n", threshold)
	for _, e := range merge.Notify {
		tag := "new"
		switch {
		case e.Reopens > 0 && e.Status == StatusReopened:
			tag = fmt.Sprintf("REOPENED x%d", e.Reopens)
		case e.SeenCount > 1:
			tag = fmt.Sprintf("standing x%d", e.SeenCount)
		}
		fmt.Fprintf(&b, "- [%s] %s (%s, %s)", strings.ToUpper(string(e.Severity)), e.Title, e.Scope, tag)
		if e.Path != "" {
			b.WriteString("\n  at: ")
			b.WriteString(e.Path)
			if e.Line > 0 {
				fmt.Fprintf(&b, ":%d", e.Line)
			}
		}
		if e.Detail != "" {
			b.WriteString("\n  ")
			b.WriteString(truncate(e.Detail, 400))
		}
		if e.SuggestedTarget != nil {
			b.WriteString("\n  suggested target: ")
			b.WriteString(e.SuggestedTarget.Name)
		}
		b.WriteString("\n  fingerprint: ")
		b.WriteString(e.Fingerprint)
		b.WriteString("\n")
	}
	if rep.Summary != "" {
		b.WriteString("\nsummary: ")
		b.WriteString(truncate(rep.Summary, 600))
		b.WriteString("\n")
	}
	b.WriteString("\n(auditor suggests; overseer disposes — file, park, or ignore-with-reason. ")
	b.WriteString("Full report and residue under state_dir/audit/.)")
	return b.String()
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
