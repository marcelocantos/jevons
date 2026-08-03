// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

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

	"github.com/marcelocantos/claudia"
)

// 🎯T207: auto-nudge stuck/idle fleet work agents (esp. after jevonsd restart).
// Pure classifier + backoff/max ledger + daemon sweep. Delivery is event_push /
// butler Deliver (resume brief). Achieved-should-reap stays T165/T195.

const (
	// DefaultIdleNudgeThreshold is how long phase=idle (or no progress) may
	// sit before a periodic wake is considered.
	DefaultIdleNudgeThreshold = 5 * time.Minute
	// DefaultIdleNudgeMax is the maximum automatic nudges per agent before
	// the product stops looping (owner may still kill). Residual: no infinite loops.
	DefaultIdleNudgeMax = 3
	// DefaultIdleNudgeInterval is how often the daemon re-evaluates the fleet.
	DefaultIdleNudgeInterval = 1 * time.Minute
	// DefaultPostRestartDelay lets StartAll settle before the first wake sweep.
	DefaultPostRestartDelay = 15 * time.Second

	compIdleNudge = "idle_nudge"
)

// DefaultIdleNudgeBackoffs is the minimum wait after nudge N before N+1
// (index 0 = after first nudge). Longer than the idle threshold so we do not
// thrash agents that already received a resume brief.
var DefaultIdleNudgeBackoffs = []time.Duration{
	10 * time.Minute,
	30 * time.Minute,
	90 * time.Minute,
}

// IdleNudgeAction is the pure-policy decision for one agent observation.
type IdleNudgeAction string

const (
	IdleNudgeSkip  IdleNudgeAction = "skip"
	IdleNudgeNudge IdleNudgeAction = "nudge"
	IdleNudgeMaxed IdleNudgeAction = "maxed"
)

// IdleNudgeKind is the payload shape once ClassifyIdleNudge returns nudge.
// Owner pin 🎯T207: first pass is brief-or-verify (full standing + mission
// brief), never a bare "continue". Short continue only when brief is known
// present (inject-once applied or session already briefed).
type IdleNudgeKind string

const (
	IdleNudgeKindFullBrief IdleNudgeKind = "full_brief"
	IdleNudgeKindContinue  IdleNudgeKind = "continue"
)

// IdleNudgeObs is a pure snapshot — no I/O. Callers assemble from registry,
// progress hub, and the nudge ledger.
type IdleNudgeObs struct {
	Name           string
	Purpose        string // work | aside | overseer (empty = work)
	ProcessRunning bool
	DeliberateStop bool // stop without kill; residual: do not force-nudge
	Phase          string
	IdleFor        time.Duration
	HasOpenMission bool // bound target still open, or post-restart open work
	DesignGated    bool
	LooksFinished  bool // leave to T165/T195 reap paths
	// BriefPresent is true when the standing fleet brief (or equivalent
	// resume brief) is known present for this worker session — e.g.
	// EnsureFleetBrief inject-once already applied, or a prior user turn
	// contained the standing brief. False ⇒ never-briefed / empty-session.
	BriefPresent   bool
	NudgeCount     int
	SinceLastNudge time.Duration // 0 when never nudged (and LastNudge zero)
	EverNudged     bool
	PostRestart    bool
	IdleThreshold  time.Duration // 0 → DefaultIdleNudgeThreshold
	MaxNudges      int           // 0 → DefaultIdleNudgeMax
	Backoffs       []time.Duration
}

// ClassifyIdleNudge decides skip | nudge | maxed for one agent.
// Hermetic oracle surface for 🎯T207.
func ClassifyIdleNudge(o IdleNudgeObs) (IdleNudgeAction, string) {
	purpose := strings.TrimSpace(o.Purpose)
	if purpose == "" {
		purpose = claudia.PurposeWork
	}
	if purpose == claudia.PurposeOverseer || purpose == claudia.PurposeAside {
		return IdleNudgeSkip, "not_work_purpose"
	}
	if o.LooksFinished {
		return IdleNudgeSkip, "achieved_should_reap"
	}
	if o.DesignGated {
		return IdleNudgeSkip, "design_gated"
	}
	if o.DeliberateStop {
		return IdleNudgeSkip, "deliberate_stop"
	}
	if !o.HasOpenMission {
		return IdleNudgeSkip, "no_open_mission"
	}
	if !o.ProcessRunning {
		// Not deliberate-stop path: dead handle without recover still skips
		// nudge (SweepDeadAgents owns rehydrate).
		return IdleNudgeSkip, "not_running"
	}

	phase := strings.ToLower(strings.TrimSpace(o.Phase))
	if phase == "working" {
		return IdleNudgeSkip, "in_progress"
	}

	max := o.MaxNudges
	if max <= 0 {
		max = DefaultIdleNudgeMax
	}
	if o.NudgeCount >= max {
		return IdleNudgeMaxed, "max_nudges"
	}

	// Backoff after a prior nudge (prevents infinite thrash).
	if o.EverNudged {
		need := NextNudgeBackoff(o.NudgeCount, o.Backoffs)
		if o.SinceLastNudge < need {
			return IdleNudgeSkip, "backoff"
		}
	}

	// Post-restart wake: outstanding non-done workers get a resume brief
	// without waiting the full idle threshold (T171 sibling).
	if o.PostRestart {
		return IdleNudgeNudge, "post_restart_wake"
	}

	threshold := o.IdleThreshold
	if threshold <= 0 {
		threshold = DefaultIdleNudgeThreshold
	}
	// phase=idle | blocked | unknown (empty) with no progress long enough.
	switch phase {
	case "idle", "blocked", "":
		if o.IdleFor >= threshold {
			return IdleNudgeNudge, "idle_stuck"
		}
		return IdleNudgeSkip, "idle_below_threshold"
	default:
		// Unknown phase strings: treat as idle-ish only when aged out.
		if o.IdleFor >= threshold {
			return IdleNudgeNudge, "idle_stuck"
		}
		return IdleNudgeSkip, "idle_below_threshold"
	}
}

// NextNudgeBackoff returns the wait required after NudgeCount successful
// nudges before another is allowed. count is the number already delivered.
func NextNudgeBackoff(nudgeCount int, backoffs []time.Duration) time.Duration {
	if len(backoffs) == 0 {
		backoffs = DefaultIdleNudgeBackoffs
	}
	if nudgeCount <= 0 {
		return 0
	}
	idx := nudgeCount - 1
	if idx >= len(backoffs) {
		idx = len(backoffs) - 1
	}
	return backoffs[idx]
}

// ClassifyIdleNudgeKind picks full_brief vs continue once a nudge is warranted.
// Hermetic: never-briefed → full_brief; briefed-then-idle → continue.
// Bare "continue" is never the first-pass default (owner pin 🎯T207).
func ClassifyIdleNudgeKind(briefPresent bool) IdleNudgeKind {
	if briefPresent {
		return IdleNudgeKindContinue
	}
	return IdleNudgeKindFullBrief
}

// IdleNudgeTextArgs builds the deliver body for a classified nudge.
type IdleNudgeTextArgs struct {
	Name        string
	TargetID    string
	Acceptance  string // optional mission acceptance (one line or multi)
	Reason      string
	PostRestart bool
	Kind        IdleNudgeKind
}

// FormatIdleNudgeText builds the resume body (without [event:] wrapper;
// butler.FormatEventPush adds that).
//
// full_brief: standing fleet brief + mission (target/acceptance) + work order.
// continue: short mid-mission resume only — requires Kind=continue (brief known).
func FormatIdleNudgeText(a IdleNudgeTextArgs) string {
	name := strings.TrimSpace(a.Name)
	if name == "" {
		name = "worker"
	}
	tid := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(a.TargetID), "🎯"))
	kind := a.Kind
	if kind == "" {
		// Fail closed: missing kind ⇒ full brief, never bare continue.
		kind = IdleNudgeKindFullBrief
	}

	if kind == IdleNudgeKindContinue {
		var b strings.Builder
		if a.PostRestart {
			b.WriteString("NUDGE (post-restart) — brief already present in this session; resume mid-mission work. ")
		} else {
			b.WriteString("NUDGE — you are phase=idle with brief already present; continue mid-mission work. ")
		}
		b.WriteString("Local master, T104 (no PR). ")
		if tid != "" {
			fmt.Fprintf(&b, "Target 🎯%s. ", tid)
		}
		if a.Reason != "" {
			fmt.Fprintf(&b, "Classifier: %s. ", a.Reason)
		}
		b.WriteString("Report commit SHA + oracle when done (or accepted-risk). Do not stay silent idle.")
		return b.String()
	}

	// full_brief — first pass / never-briefed / empty-session / post-restart rehydrate
	var b strings.Builder
	b.WriteString(FleetStandingBrief)
	if a.PostRestart {
		b.WriteString("NUDGE (post-restart wake / rehydrate) — jevonsd recovered. ")
	} else {
		b.WriteString("NUDGE (first-pass / never-briefed) — you sat phase=idle with no verified mission brief. ")
	}
	b.WriteString("This message injects the standing fleet brief (or confirms it). Do not treat a one-word continue as your brief.\n\n")
	fmt.Fprintf(&b, "Agent: %s\n", name)
	if tid != "" {
		fmt.Fprintf(&b, "Mission target: 🎯%s\n", tid)
	} else {
		b.WriteString("Mission target: (none bound — resume open work for this agent name)\n")
	}
	if acc := strings.TrimSpace(a.Acceptance); acc != "" {
		fmt.Fprintf(&b, "Acceptance:\n%s\n", acc)
	}
	b.WriteString("\nLocal master only (T104). No PR unless owner opened Ship.\n")
	if a.Reason != "" {
		fmt.Fprintf(&b, "Classifier: %s.\n", a.Reason)
	}
	b.WriteString("Continue from any partial work; report commit SHA + oracle evidence when done (or accepted-risk). Do not stay silent idle.")
	return b.String()
}

// IdleNudgeEventSource is the event kind for PushEvent / deliver.
func IdleNudgeEventSource(postRestart bool, kind IdleNudgeKind) string {
	if kind == IdleNudgeKindFullBrief {
		if postRestart {
			return "post-restart-brief"
		}
		return "idle-nudge-brief"
	}
	if postRestart {
		return "post-restart-wake"
	}
	return "idle-nudge"
}

// SessionLooksBriefed reports whether transcript/user text already carries
// the standing fleet brief (or inject marker). Pure string heuristic.
func SessionLooksBriefed(priorUserText string) bool {
	s := strings.TrimSpace(priorUserText)
	if s == "" {
		return false
	}
	return strings.Contains(s, "Jevons fleet standing brief") ||
		strings.Contains(s, "fleet standing brief")
}

// --- activity tracker (last progress per agent) ---

// IdleActivity is the last glanceable phase/time for idle classification.
type IdleActivity struct {
	Phase   string
	Updated time.Time
}

// IdleActivityTracker records ACP-derived phase for the idle-nudge sweep.
type IdleActivityTracker struct {
	mu  sync.Mutex
	by  map[string]IdleActivity
	now func() time.Time
}

// NewIdleActivityTracker returns an empty tracker.
func NewIdleActivityTracker() *IdleActivityTracker {
	return &IdleActivityTracker{by: make(map[string]IdleActivity)}
}

func (t *IdleActivityTracker) clock() time.Time {
	if t != nil && t.now != nil {
		return t.now()
	}
	return time.Now()
}

// Observe updates phase from a claudia event (same signals as AgentProgress).
func (t *IdleActivityTracker) Observe(name string, ev claudia.Event) {
	if t == nil || name == "" {
		return
	}
	phase, ok := idlePhaseFromEvent(ev)
	if !ok {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.by == nil {
		t.by = make(map[string]IdleActivity)
	}
	t.by[name] = IdleActivity{Phase: phase, Updated: t.clock()}
}

// Get returns the last activity, or zero.
func (t *IdleActivityTracker) Get(name string) IdleActivity {
	if t == nil {
		return IdleActivity{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.by[name]
}

// SeedRunning marks a just-wired running agent as idle at now when unknown
// (post-StartAll baseline so post-restart wake sees phase=idle).
func (t *IdleActivityTracker) SeedRunning(name string) {
	if t == nil || name == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.by == nil {
		t.by = make(map[string]IdleActivity)
	}
	if prev, ok := t.by[name]; ok && prev.Phase != "" {
		return
	}
	t.by[name] = IdleActivity{Phase: "idle", Updated: t.clock()}
}

func idlePhaseFromEvent(ev claudia.Event) (string, bool) {
	switch {
	case ev.Type == "progress" && ev.ProgressType != "":
		return "working", true
	case ev.IsTerminalStop():
		return "idle", true
	case ev.Type == "assistant", ev.Type == "user":
		return "working", true
	default:
		return "", false
	}
}

// --- durable nudge ledger (backoff/max across restarts) ---

type idleNudgeLedgerFile struct {
	Agents map[string]idleNudgeEntry `json:"agents"`
}

type idleNudgeEntry struct {
	Count     int       `json:"count"`
	LastNudge time.Time `json:"last_nudge"`
}

// IdleNudgeLedger persists per-agent nudge counts under state_dir.
type IdleNudgeLedger struct {
	path string
	mu   sync.Mutex
	data idleNudgeLedgerFile
}

// OpenIdleNudgeLedger loads or creates the ledger at stateDir/fleet/idle_nudge.json.
func OpenIdleNudgeLedger(stateDir string) (*IdleNudgeLedger, error) {
	if strings.TrimSpace(stateDir) == "" {
		return nil, fmt.Errorf("idle nudge: StateDir required")
	}
	dir := filepath.Join(stateDir, "fleet")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "idle_nudge.json")
	l := &IdleNudgeLedger{path: path, data: idleNudgeLedgerFile{Agents: map[string]idleNudgeEntry{}}}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return l, nil
		}
		return nil, err
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &l.data); err != nil {
			return nil, fmt.Errorf("idle nudge ledger: %w", err)
		}
	}
	if l.data.Agents == nil {
		l.data.Agents = map[string]idleNudgeEntry{}
	}
	return l, nil
}

// Get returns count and last-nudge time for name.
func (l *IdleNudgeLedger) Get(name string) (count int, last time.Time) {
	if l == nil {
		return 0, time.Time{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.data.Agents[name]
	return e.Count, e.LastNudge
}

// Record increments count and sets last nudge time; atomic write-and-rename.
func (l *IdleNudgeLedger) Record(name string, at time.Time) error {
	if l == nil || name == "" {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.data.Agents == nil {
		l.data.Agents = map[string]idleNudgeEntry{}
	}
	e := l.data.Agents[name]
	e.Count++
	e.LastNudge = at.UTC()
	l.data.Agents[name] = e
	return l.persistLocked()
}

// Reset clears an agent (e.g. after kill/deregister). Best-effort.
func (l *IdleNudgeLedger) Reset(name string) error {
	if l == nil || name == "" {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.data.Agents, name)
	return l.persistLocked()
}

func (l *IdleNudgeLedger) persistLocked() error {
	b, err := json.MarshalIndent(l.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, l.path)
}

// --- sweep + daemon loop ---

// IdleNudgeReport is one agent evaluation outcome.
type IdleNudgeReport struct {
	Name        string
	Action      IdleNudgeAction
	Reason      string
	Kind        IdleNudgeKind
	Delivered   bool
	Error       string
	PostRestart bool
}

// IdleNudgePusher delivers a resume brief (production: butler.PushEvent).
type IdleNudgePusher func(target, event, text string) error

// IdleNudgeSweepArgs configures one pure-ish fleet evaluation.
type IdleNudgeSweepArgs struct {
	Reg          *claudia.Registry
	Activity     *IdleActivityTracker
	Ledger       *IdleNudgeLedger
	Push         IdleNudgePusher
	Now          time.Time
	PostRestart  bool
	OverseerName string
	// BriefPresent is optional: agent name → standing brief known present
	// (fleetBriefed inject-once or session already has brief). Nil = never
	// present → full_brief on first nudge.
	BriefPresent func(name string) bool
	// MarkBriefed is optional: called after a successful full_brief deliver
	// so inject-once / fleetBriefed stays coherent with agent_send.
	MarkBriefed func(name string)
	// DesignGated is optional: targetID → design-gated. Nil = never gated.
	DesignGated func(targetID string) bool
	// MissionOpen is optional: targetID → still open. Nil = any non-empty TargetID is open;
	// post-restart also treats purpose=work without TargetID as open work.
	MissionOpen func(targetID string) bool
	// LastTerminalReport optional: name → last terminal text for LooksFinished.
	LastTerminalReport func(name string) string
	// MissionAcceptance optional: targetID → acceptance text for full brief.
	MissionAcceptance func(targetID string) string
	// ProcessRunning optional override (hermetic tests without OS processes).
	// Nil → reg.Get(name).Alive().
	ProcessRunning func(name string) bool
}

// SweepIdleNudges classifies every registered agent and delivers nudges.
// Pure policy decisions are always recorded; Push is only called for nudge.
func SweepIdleNudges(args IdleNudgeSweepArgs) []IdleNudgeReport {
	if args.Reg == nil {
		return nil
	}
	now := args.Now
	if now.IsZero() {
		now = time.Now()
	}
	overseer := args.OverseerName
	if overseer == "" {
		overseer = "jevons"
	}
	var out []IdleNudgeReport
	for _, d := range args.Reg.List() {
		if d.Name == "" || d.Name == overseer {
			continue
		}
		rep := evaluateAndMaybeNudge(d, args, now)
		if rep.Action == IdleNudgeSkip && rep.Reason == "not_work_purpose" {
			continue // quiet skip for asides
		}
		out = append(out, rep)
	}
	return out
}

func evaluateAndMaybeNudge(d claudia.AgentDef, args IdleNudgeSweepArgs, now time.Time) IdleNudgeReport {
	purpose := d.Purpose
	if purpose == "" {
		purpose = claudia.PurposeWork
	}
	running := false
	if args.ProcessRunning != nil {
		running = args.ProcessRunning(d.Name)
	} else if proc := args.Reg.Get(d.Name); proc != nil {
		running = proc.Alive()
	}
	// Deliberate stop: registered, not running, and not a dead AutoStart
	// handle we expect SweepDeadAgents to rehydrate — stop without kill.
	deliberateStop := !running && !d.AutoStart

	act := IdleActivity{}
	if args.Activity != nil {
		act = args.Activity.Get(d.Name)
	}
	idleFor := time.Duration(0)
	if !act.Updated.IsZero() {
		idleFor = now.Sub(act.Updated)
	} else if running {
		// No ACP events yet after rehydrate — treat as idle since unknown.
		// Post-restart still wakes; periodic needs threshold → use large IdleFor
		// only when PostRestart, else 0 (below threshold until activity seeds).
		if args.PostRestart {
			idleFor = DefaultIdleNudgeThreshold
		}
	}

	count := 0
	var last time.Time
	if args.Ledger != nil {
		count, last = args.Ledger.Get(d.Name)
	}
	since := time.Duration(0)
	ever := !last.IsZero()
	if ever {
		since = now.Sub(last)
	}

	targetID := strings.TrimSpace(d.TargetID)
	designGated := false
	if args.DesignGated != nil && targetID != "" {
		designGated = args.DesignGated(targetID)
	}

	hasMission := false
	if targetID != "" {
		if args.MissionOpen != nil {
			hasMission = args.MissionOpen(targetID)
		} else {
			hasMission = true
		}
	} else if args.PostRestart && purpose == claudia.PurposeWork {
		// Outstanding non-done workers without explicit TargetID still wake
		// after restart (owner incident: jv-t* silent idle).
		hasMission = true
	} else if purpose == claudia.PurposeWork && targetID == "" {
		// Periodic path: work agents without target_id still eligible when
		// idle long enough (registry rows that never bound T198).
		hasMission = true
	}

	looksFinished := false
	if args.LastTerminalReport != nil {
		if r := args.LastTerminalReport(d.Name); r != "" {
			looksFinished = LooksLikeFinishedWorkReport(r)
		}
	}

	briefPresent := false
	if args.BriefPresent != nil {
		briefPresent = args.BriefPresent(d.Name)
	}

	obs := IdleNudgeObs{
		Name:           d.Name,
		Purpose:        purpose,
		ProcessRunning: running,
		DeliberateStop: deliberateStop,
		Phase:          act.Phase,
		IdleFor:        idleFor,
		HasOpenMission: hasMission,
		DesignGated:    designGated,
		LooksFinished:  looksFinished,
		BriefPresent:   briefPresent,
		NudgeCount:     count,
		SinceLastNudge: since,
		EverNudged:     ever,
		PostRestart:    args.PostRestart,
	}
	action, reason := ClassifyIdleNudge(obs)
	kind := ClassifyIdleNudgeKind(briefPresent)
	rep := IdleNudgeReport{
		Name:        d.Name,
		Action:      action,
		Reason:      reason,
		Kind:        kind,
		PostRestart: args.PostRestart,
	}
	if action != IdleNudgeNudge {
		return rep
	}
	if args.Push == nil {
		rep.Error = "no_pusher"
		return rep
	}
	acceptance := ""
	if args.MissionAcceptance != nil && targetID != "" {
		acceptance = args.MissionAcceptance(targetID)
	}
	text := FormatIdleNudgeText(IdleNudgeTextArgs{
		Name:        d.Name,
		TargetID:    targetID,
		Acceptance:  acceptance,
		Reason:      reason,
		PostRestart: args.PostRestart,
		Kind:        kind,
	})
	// Keep inject-once coherent: full_brief marks fleet briefed after send.
	// (EnsureFleetBrief on agent_send also marks; we set after successful push.)
	event := IdleNudgeEventSource(args.PostRestart, kind)
	if err := args.Push(d.Name, event, text); err != nil {
		rep.Error = err.Error()
		slog.Warn("idle nudge deliver failed", "agent", d.Name, "err", err)
		return rep
	}
	rep.Delivered = true
	if kind == IdleNudgeKindFullBrief && args.MarkBriefed != nil {
		args.MarkBriefed(d.Name)
	}
	if args.Ledger != nil {
		if err := args.Ledger.Record(d.Name, now); err != nil {
			slog.Warn("idle nudge ledger record failed", "agent", d.Name, "err", err)
		}
	}
	// After a successful nudge, mark activity so we do not immediately
	// re-classify as aged idle before the agent responds.
	if args.Activity != nil {
		args.Activity.mu.Lock()
		if args.Activity.by == nil {
			args.Activity.by = make(map[string]IdleActivity)
		}
		args.Activity.by[d.Name] = IdleActivity{Phase: "working", Updated: now}
		args.Activity.mu.Unlock()
	}
	slog.Info("idle nudge delivered",
		"agent", d.Name, "reason", reason, "kind", kind, "post_restart", args.PostRestart)
	return rep
}

// IdleNudgeLoopArgs configures the daemon schedule (🎯T207).
type IdleNudgeLoopArgs struct {
	Server       *Server
	StateDir     string
	Activity     *IdleActivityTracker
	Interval     time.Duration // 0 → DefaultIdleNudgeInterval; <0 disables ticker (post-restart only)
	PostDelay    time.Duration // 0 → DefaultPostRestartDelay
	OverseerName string
	Now          func() time.Time
}

// StartIdleNudgeLoop runs post-restart wake then periodic sweeps until ctx done.
// Safe to call from a goroutine. No-ops when Server or registry is nil.
func StartIdleNudgeLoop(ctx context.Context, args IdleNudgeLoopArgs) {
	if args.Server == nil || args.Server.registry == nil {
		return
	}
	ledger, err := OpenIdleNudgeLedger(args.StateDir)
	if err != nil {
		slog.Warn("idle nudge ledger disabled", "err", err)
		ledger = nil
	}
	activity := args.Activity
	if activity == nil {
		activity = NewIdleActivityTracker()
	}
	// Expose tracker on server for event sink updates.
	args.Server.mu.Lock()
	args.Server.idleActivity = activity
	args.Server.idleNudgeLedger = ledger
	args.Server.mu.Unlock()

	nowFn := args.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	overseer := args.OverseerName
	if overseer == "" {
		overseer = "jevons"
	}
	postDelay := args.PostDelay
	if postDelay == 0 {
		postDelay = DefaultPostRestartDelay
	}

	push := func(target, event, text string) error {
		if args.Server.butler == nil {
			return fmt.Errorf("butler not configured")
		}
		_, err := args.Server.butler.PushEvent(target, event, text)
		if err != nil {
			return err
		}
		args.Server.logLifecycle(compIdleNudge, "nudge", "ok", map[string]any{
			"target": target, "event": event,
		})
		return nil
	}

	briefPresent := func(name string) bool {
		args.Server.mu.Lock()
		defer args.Server.mu.Unlock()
		return args.Server.fleetBriefed != nil && args.Server.fleetBriefed[name]
	}
	markBriefed := func(name string) {
		args.Server.mu.Lock()
		defer args.Server.mu.Unlock()
		if args.Server.fleetBriefed == nil {
			args.Server.fleetBriefed = map[string]bool{}
		}
		args.Server.fleetBriefed[name] = true
	}

	runSweep := func(postRestart bool) {
		// Dead-handle rehydrate first so post-restart wake sees running procs.
		if reps := SweepDeadAgents(args.Server.registry, overseer); len(reps) > 0 {
			slog.Info("idle nudge: fleet health before sweep", "report", FormatDeadAgentReport(reps))
		}
		// Seed activity for currently running workers (no ACP yet).
		for _, d := range args.Server.registry.List() {
			if proc := args.Server.registry.Get(d.Name); proc != nil && proc.Alive() {
				activity.SeedRunning(d.Name)
			}
		}
		reps := SweepIdleNudges(IdleNudgeSweepArgs{
			Reg:          args.Server.registry,
			Activity:     activity,
			Ledger:       ledger,
			Push:         push,
			Now:          nowFn(),
			PostRestart:  postRestart,
			OverseerName: overseer,
			BriefPresent: briefPresent,
			MarkBriefed:  markBriefed,
		})
		var nudged, maxed, failed, fullBrief int
		for _, r := range reps {
			switch {
			case r.Delivered:
				nudged++
				if r.Kind == IdleNudgeKindFullBrief {
					fullBrief++
				}
			case r.Action == IdleNudgeMaxed:
				maxed++
			case r.Error != "":
				failed++
			}
		}
		slog.Info("idle nudge sweep",
			"post_restart", postRestart,
			"evaluated", len(reps),
			"nudged", nudged,
			"full_brief", fullBrief,
			"maxed", maxed,
			"failed", failed,
		)
	}

	// Post-restart wake after short settle.
	select {
	case <-ctx.Done():
		return
	case <-time.After(postDelay):
		runSweep(true)
	}

	interval := args.Interval
	if interval == 0 {
		interval = DefaultIdleNudgeInterval
	}
	if interval < 0 {
		<-ctx.Done()
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runSweep(false)
		}
	}
}
