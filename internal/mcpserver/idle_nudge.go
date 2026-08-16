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
	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/wakebatch"
)

// 🎯T207: auto-nudge stuck/idle fleet work agents (esp. after jevonsd restart).
// Pure classifier + backoff/max ledger + daemon sweep.
//
// Delivery MUST be fire-and-forget (send / queue / interrupt), never
// butler.PushEvent → fleet.Deliver → WaitForResponse. The latter blocks up to
// ~10m per agent and serializes the whole fleet wake — live failure mode:
// one ledger entry, no "idle nudge sweep" summary, remaining workers stay
// phase=idle forever until a manual PO wave.
// Achieved-should-reap stays T165/T195.

const (
	// DefaultIdleNudgeThreshold is how long phase=idle (or no progress) may
	// sit before a periodic wake is considered.
	// Tuned for continuous pressure: 5m left unproductive workers sitting too long.
	DefaultIdleNudgeThreshold = 2 * time.Minute
	// DefaultIdleNudgeMax is the maximum automatic nudges per agent before
	// the product stops looping (owner may still kill). Residual: no infinite loops.
	DefaultIdleNudgeMax = 8
	// DefaultIdleNudgeInterval is how often the daemon re-evaluates the fleet.
	// 30s keeps pressure on idle workers; cockpit also ticks fleet hooks (🎯T204).
	DefaultIdleNudgeInterval = 30 * time.Second
	// DefaultIdlePressureInterval is how often the daemon re-evaluates open-mission
	// work agents for idle re-pressure (🎯T315). Documented bound: a worker that
	// enters phase=idle with an open mission is re-pressured within
	// DefaultIdleNudgeThreshold + DefaultIdlePressureInterval (≤ 3m), then per
	// DefaultIdleNudgeBackoffs, capped at DefaultIdleNudgeMax. Minutes, not hours.
	DefaultIdlePressureInterval = time.Minute
	// DefaultPostRestartDelay lets StartAll settle before the first wake sweep.
	DefaultPostRestartDelay = 15 * time.Second

	compIdleNudge = "idle_nudge"
)

// DefaultIdleNudgeBackoffs is the minimum wait after nudge N before N+1
// (index 0 = after first nudge). Must be short enough that a worker which
// stays phase=idle after a brief is re-pressured within the same session,
// but long enough to avoid thrashing mid-tool turns that look idle briefly.
var DefaultIdleNudgeBackoffs = []time.Duration{
	2 * time.Minute,
	5 * time.Minute,
	15 * time.Minute,
}

// IdleNudgeAction is the pure-policy decision for one agent observation.
type IdleNudgeAction string

const (
	IdleNudgeSkip  IdleNudgeAction = "skip"
	IdleNudgeNudge IdleNudgeAction = "nudge"
	IdleNudgeMaxed IdleNudgeAction = "maxed"
)

// Skip reasons the 🎯T317 impatience ladder reads back off this sweep. Only
// these three distinguish "the gap is still open, the actuator is just quiet
// this tick" from "the agent came back to work"; every other skip reason means
// the agent is not an impatience gap at all.
const (
	// IdleSkipInProgress: phase=working — the satisfaction verdict.
	IdleSkipInProgress = "in_progress"
	// IdleSkipBackoff: still idle, actuator waiting out its backoff.
	IdleSkipBackoff = "backoff"
	// IdleSkipBelowThreshold: still idle, not yet aged past the threshold.
	IdleSkipBelowThreshold = "idle_below_threshold"
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
	Name    string
	Purpose string // work | aside | overseer (empty = work)
	// FleetIntent / Intent are the deliberate answers to "should this be
	// running?" (🎯T414). Empty resolves to working, so an unstamped agent
	// behaves exactly as it did before intent existed.
	FleetIntent    fleetintent.State
	Intent         fleetintent.State
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
	// 🎯T414: intent outranks every observation below. A parked, blocked, or
	// reaped agent is idle *on purpose*, and nudging it is the fault this
	// classifier used to commit — it read "phase=idle, mission open" and had
	// no way to ask whether the agent was supposed to be working at all.
	if d := fleetintent.Allows(o.FleetIntent, o.Intent, fleetintent.ControlNudge); !d.Allow {
		return IdleNudgeSkip, d.Reason
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
		return IdleNudgeSkip, IdleSkipInProgress
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
			return IdleNudgeSkip, IdleSkipBackoff
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
		return IdleNudgeSkip, IdleSkipBelowThreshold
	default:
		// Unknown phase strings: treat as idle-ish only when aged out.
		if o.IdleFor >= threshold {
			return IdleNudgeNudge, "idle_stuck"
		}
		return IdleNudgeSkip, IdleSkipBelowThreshold
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
	tid := FormatTargetID(a.TargetID)
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
		b.WriteString("Local master, 🎯T104 (no PR). ")
		if tid != "" {
			fmt.Fprintf(&b, "Target %s. ", tid)
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
		fmt.Fprintf(&b, "Mission target: %s\n", tid)
	} else {
		b.WriteString("Mission target: (none bound — resume open work for this agent name)\n")
	}
	if acc := strings.TrimSpace(a.Acceptance); acc != "" {
		fmt.Fprintf(&b, "Acceptance:\n%s\n", acc)
	}
	b.WriteString("\nLocal master only (🎯T104). No PR unless owner opened Ship.\n")
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

// formatIdleNudgeWire wraps the resume body like butler.FormatEventPush so
// workers can distinguish automated idle pressure from owner chat.
// Kept local so the fire-and-forget send path does not need a Butler.
func formatIdleNudgeWire(event, text string) string {
	src := strings.TrimSpace(event)
	if src == "" {
		src = "idle-nudge"
	}
	return fmt.Sprintf("[event: %s] %s", src, strings.TrimSpace(text))
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

// IdleActivity is the last glanceable phase/time for idle classification
// and fleet recover (🎯T236) latches.
type IdleActivity struct {
	Phase   string
	Updated time.Time
	// FailureClass is the last structured terminal failure (agenterr / T237).
	FailureClass agenterr.Class
	// NeedsRecover is true after terminal error/empty until ClearRecover.
	NeedsRecover bool
	// TerminalEmpty is true when the last terminal stop had empty text.
	TerminalEmpty bool
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
	_, _, _ = t.ObserveTransition(name, ev)
}

// ObserveTransition updates phase and reports whether the agent entered idle
// from a prior working phase (🎯T207 enter-idle edge — not seed-idle).
// Purpose/open-mission filters are applied by the caller.
func (t *IdleActivityTracker) ObserveTransition(name string, ev claudia.Event) (prevPhase, nextPhase string, enteredIdle bool) {
	if t == nil || name == "" {
		return "", "", false
	}
	phase, ok := idlePhaseFromEvent(ev)
	if !ok {
		return "", "", false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.by == nil {
		t.by = make(map[string]IdleActivity)
	}
	prev := t.by[name]
	prevPhase = prev.Phase
	nextPhase = phase
	// Preserve T236 recover latches across mid-turn working updates;
	// NoteTerminalOutcome sets them on end_turn; ClearRecover clears.
	next := IdleActivity{
		Phase:         phase,
		Updated:       t.clock(),
		FailureClass:  prev.FailureClass,
		NeedsRecover:  prev.NeedsRecover,
		TerminalEmpty: prev.TerminalEmpty,
	}
	// Fresh working progress clears stale recover latch only when we see
	// real tool/assistant activity after a prior recover cycle was delivered
	// (NeedsRecover already false). If NeedsRecover is still true, keep it
	// until ClearRecover so a flappy assistant crumb cannot drop the signal.
	t.by[name] = next
	enteredIdle = nextPhase == "idle" && strings.ToLower(strings.TrimSpace(prevPhase)) == "working"
	return prevPhase, nextPhase, enteredIdle
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
	// Eligible optionally pre-filters agents before classification (🎯T315).
	// False ⇒ skip with reason not_open_mission. Nil = every registered agent
	// is classified. The periodic pressure path uses it for T244 (unbound
	// PO/boss with no work children is standing idle) and T330 (PO/boss with
	// engaged implementers is sleep-OK — no re-continue thrash).
	Eligible func(d claudia.AgentDef) bool
	// Intent is the deliberate fleet intent snapshot (🎯T414). The zero value
	// is all-working, so a sweep assembled without one nudges exactly as it
	// did before intent existed.
	Intent fleetintent.Snapshot
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
	if args.Eligible != nil && !args.Eligible(d) {
		return IdleNudgeReport{
			Name:        d.Name,
			Action:      IdleNudgeSkip,
			Reason:      "not_open_mission",
			PostRestart: args.PostRestart,
		}
	}
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

	// 🎯T171: post-restart short resume only for open-mission implementers
	// (not PO/boss, not missionless non-AutoStart). Periodic path unchanged.
	if args.PostRestart {
		if !EligibleOpenMissionResume(d, running, deliberateStop, designGated, looksFinished, args.Intent) {
			hasMission = false
		}
	}

	obs := IdleNudgeObs{
		Name:           d.Name,
		Purpose:        purpose,
		FleetIntent:    args.Intent.FleetState(),
		Intent:         args.Intent.AgentState(d.Name),
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
	if args.PostRestart && action == IdleNudgeNudge && !EligibleOpenMissionResume(d, running, deliberateStop, designGated, looksFinished, args.Intent) {
		action, reason = IdleNudgeSkip, "not_open_mission_resume"
	}
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
	// Reset idle clock only — do NOT claim phase=working. Fake "working"
	// poisoned re-nudge forever when ACP never observed a real turn (live:
	// post-nudge workers stayed phase=idle in UI but classifier skipped
	// in_progress). Backoff/max already prevent thrash.
	// Preserve T236 FailureClass; clear NeedsRecover via ClearRecover shape.
	if args.Activity != nil {
		args.Activity.ClearRecover(d.Name, now)
	}
	slog.Info("idle nudge delivered",
		"agent", d.Name, "reason", reason, "kind", kind, "post_restart", args.PostRestart)
	return rep
}

// IdleNudgeLoopArgs configures the 🎯T171 dual-path supervisor (T207 send path).
// Steady-state: enter-idle → parent PO only. Restart: dual path (events + short resume).
type IdleNudgeLoopArgs struct {
	Server       *Server
	StateDir     string
	Activity     *IdleActivityTracker
	Interval     time.Duration // unused for nudge; fleet health tick only (0 → 1m)
	PostDelay    time.Duration // 0 → DefaultDaemonRestartNotifyDelay
	OverseerName string
	DefaultPO    string // empty → jevons-po
	Now          func() time.Time
}

// StartIdleNudgeLoop wires enter-idle tracking and, after settle, runs 🎯T171 dual path:
//
//  1. daemon-restarted → each parent PO + overseer
//  2. short fire-and-forget resume → open-mission work agents (T207 brief-or-verify)
//  3. steady-state worker-idle remains transition-only via emitWorkerIdleToParent
//
// Does NOT periodic-poll nudge. Does NOT blast missionless/aside/overseer.
func StartIdleNudgeLoop(ctx context.Context, args IdleNudgeLoopArgs) {
	if args.Server == nil || args.Server.registry == nil {
		return
	}
	activity := args.Activity
	if activity == nil {
		activity = NewIdleActivityTracker()
	}
	args.Server.mu.Lock()
	args.Server.idleActivity = activity
	if args.Server.idleEventLast == nil {
		args.Server.idleEventLast = map[string]time.Time{}
	}
	args.Server.mu.Unlock()

	overseer := args.OverseerName
	if overseer == "" {
		overseer = "jevons"
	}
	defaultPO := args.DefaultPO
	if defaultPO == "" {
		defaultPO = defaultProductPOName
	}
	postDelay := args.PostDelay
	if postDelay == 0 {
		postDelay = DefaultDaemonRestartNotifyDelay
	}
	stateDir := args.StateDir

	// Seed activity so boot seed-idle is not confused with working→idle.
	for _, d := range args.Server.registry.List() {
		if proc := args.Server.registry.Get(d.Name); proc != nil && proc.Alive() {
			activity.SeedRunning(d.Name)
		}
	}

	// Durable backoff/max ledger for the re-pressure actuator (🎯T315). Opened
	// here so the periodic path has it before the first tick; the post-restart
	// sweep reuses it rather than opening a second in-memory copy of the file.
	if stateDir != "" {
		if l, err := OpenIdleNudgeLedger(stateDir); err == nil {
			args.Server.mu.Lock()
			args.Server.idleNudgeLedger = l
			args.Server.mu.Unlock()
		} else {
			slog.Warn("idle pressure: ledger open failed", "err", err)
		}
	}

	// Cockpit fleet hook: dead-handle health + T236 outage/stuck recover +
	// 🎯T315 open-mission idle re-pressure. Event-first to the parent PO
	// (emitWorkerIdleToParent) stays for replan/reap, but it is no longer the
	// only pressure path — a PO that is itself idle or queued used to leave
	// implementers silent for hours.
	args.Server.mu.Lock()
	args.Server.idleNudgeSweep = func(postRestart bool) {
		// 🎯T418: mute from the stuck snapshot before SweepDeadAgents /
		// recover relaunches a rescuer.
		args.Server.SweepSendBacklogs()
		args.Server.SweepHandovers()
		args.Server.reportFleetMuteIfNeeded()
		if reps := SweepDeadAgents(args.Server.registry, overseer, args.Server.fleetIntent()); len(reps) > 0 {
			slog.Info("fleet health (cockpit/idle loop)", "report", FormatDeadAgentReport(reps), "post_restart", postRestart)
		}
		args.Server.runFleetRecoverSweep(postRestart)
		args.Server.TriggerIdlePressureSweep()
	}
	args.Server.mu.Unlock()

	// After settle: dual path (events to PO+overseer, short resume to open-mission workers).
	// 🎯T328: overseer also gets owner-intent-resume when chatlog has open work.
	select {
	case <-ctx.Done():
		return
	case <-time.After(postDelay):
		args.Server.NotifyDaemonRestarted(overseer, defaultPO, stateDir)
		args.Server.ResumeOpenMissionWorkers(overseer, stateDir, activity)
	}

	// Periodic fleet health + 🎯T315 open-mission idle re-pressure.
	interval := args.Interval
	if interval == 0 {
		interval = DefaultIdlePressureInterval
	}
	if interval < 0 {
		<-ctx.Done()
		return
	}
	runIdlePressureLoop(ctx, interval, func() {
		if reps := SweepDeadAgents(args.Server.registry, overseer, args.Server.fleetIntent()); len(reps) > 0 {
			slog.Info("fleet health periodic", "report", FormatDeadAgentReport(reps))
		}
		args.Server.TriggerIdlePressureSweep()
		// 🎯T392.2: deliver any digests whose window has elapsed. Driven
		// from the sweep that generates the events rather than its own
		// timer — a second ticker would be a second thing to get wrong,
		// and the flush is cheap when nothing is pending.
		args.Server.FlushWakeBatches()
	})
}

// runIdlePressureLoop ticks the actuator until ctx is done. Extracted from
// StartIdleNudgeLoop so the periodic path is hermetically testable without a
// daemon, registry, or clock skew (🎯T315).
func runIdlePressureLoop(ctx context.Context, interval time.Duration, tick func()) {
	if tick == nil || interval <= 0 {
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
			tick()
		}
	}
}

// IdlePressureHooks are the optional collaborator seams of the 🎯T315
// actuator. All are nil-safe; nil means the actuator falls back to its own
// conservative defaults (any bound TargetID counts as an open mission,
// nothing is design-gated, a maxed agent is logged and left alone).
//
//   - MissionOpen / LooksSatisfied: satisfaction semantics — owned by 🎯T316.
//   - OnMaxed: escalation/noise ladder for an agent that exhausted its nudge
//     budget while still idle — owned by 🎯T317.
type IdlePressureHooks struct {
	MissionOpen    func(targetID string) bool
	DesignGated    func(targetID string) bool
	LooksSatisfied func(name string) string // last terminal report, "" = unknown
	OnMaxed        func(rep IdleNudgeReport)
}

// missionOpenHook returns the installed satisfaction seam, or nil when no
// ledger is wired (hermetic servers, daemons that never called
// SetIdlePressureHooks). Nil keeps the conservative default: any bound target
// counts as open.
func (s *Server) missionOpenHook() func(targetID string) bool {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.idlePressureHooks.MissionOpen
}

// memoMissionOpen caches answers for the duration of one classification pass.
// The live hook walks the registry and re-parses a bullseye.yaml per call, and
// a single emit decision asks about the agent plus every child of its parent.
// Staleness within one pass is not a risk worth an uncached read: a target
// does not change hands mid-decision.
func memoMissionOpen(f func(targetID string) bool) func(targetID string) bool {
	if f == nil {
		return nil
	}
	cache := map[string]bool{}
	return func(targetID string) bool {
		if v, ok := cache[targetID]; ok {
			return v
		}
		v := f(targetID)
		cache[targetID] = v
		return v
	}
}

// SetIdlePressureHooks installs the collaborator seams. Safe to call with a
// zero value to clear them.
func (s *Server) SetIdlePressureHooks(h IdlePressureHooks) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.idlePressureHooks = h
	s.mu.Unlock()
}

// idlePressureDeps overrides I/O for hermetic tests. Zero value = live path:
// wall clock, fire-and-forget send, registry process liveness.
type idlePressureDeps struct {
	Now     time.Time
	Push    IdleNudgePusher
	Running func(name string) bool
}

// TriggerIdlePressureSweep runs one periodic open-mission idle re-pressure
// pass over the live fleet (🎯T315). Fire-and-forget; never blocks on a reply.
func (s *Server) TriggerIdlePressureSweep() {
	s.idlePressureSweep(idlePressureDeps{})
}

// idlePressureSweep is the daemon re-pressure actuator: every open-mission
// work agent that is running and has sat phase=idle past the threshold gets a
// real brief-or-continue deliver, with no overseer or PO turn in the loop.
// Backoff and max-nudges come from the durable ledger, so this cannot thrash
// or run forever.
//
// When SetImpatienceEngine is installed (🎯T317), this sweep drives the T316
// reconcile set and the impatience ladder instead: re-pressure, overseer
// noise, and human sticky all fire through the ladder sinks. The T315-only
// SweepIdleNudges path remains for hermetic tests and daemons that have not
// wired the ladder yet.
func (s *Server) idlePressureSweep(deps idlePressureDeps) []IdleNudgeReport {
	if s == nil || s.registry == nil {
		return nil
	}
	overseer := s.overseerName()
	if overseer == "" {
		overseer = "jevons"
	}

	s.mu.Lock()
	activity := s.idleActivity
	ledger := s.idleNudgeLedger
	hooks := s.idlePressureHooks
	eng := s.impatience
	s.mu.Unlock()
	if activity == nil {
		activity = NewIdleActivityTracker()
		s.mu.Lock()
		s.idleActivity = activity
		s.mu.Unlock()
	}

	now := deps.Now
	if now.IsZero() {
		now = time.Now()
	}

	// 🎯T317 path: standing set + ladder owns re-pressure and escalation.
	if eng != nil {
		eng.tick(s, deps, hooks)
		return nil
	}

	push := deps.Push
	if push == nil {
		push = func(target, event, text string) error {
			// interrupt=false: a worker mid-turn is phase=working and never
			// classified idle, so queueing is the right behaviour if the send
			// races a turn start. Interrupting live turns is T236's job.
			_, err := s.sendDaemonComposed(target, formatIdleNudgeWire(event, text), false)
			return err
		}
	}

	defs := s.registry.List()
	reps := SweepIdleNudges(IdleNudgeSweepArgs{
		Reg:          s.registry,
		Activity:     activity,
		Ledger:       ledger,
		Push:         push,
		Now:          now,
		PostRestart:  false,
		OverseerName: overseer,
		BriefPresent: func(name string) bool {
			s.mu.Lock()
			defer s.mu.Unlock()
			return s.fleetBriefed != nil && s.fleetBriefed[name]
		},
		MarkBriefed: func(name string) {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.fleetBriefed == nil {
				s.fleetBriefed = map[string]bool{}
			}
			s.fleetBriefed[name] = true
		},
		MissionOpen:        hooks.MissionOpen,
		DesignGated:        hooks.DesignGated,
		LastTerminalReport: hooks.LooksSatisfied,
		ProcessRunning:     deps.Running,
		Intent:             s.fleetIntent(),
		Eligible: func(d claudia.AgentDef) bool {
			// 🎯T244: unbound PO/boss with no work children is standing idle.
			// 🎯T330: PO/boss with engaged implementers is sleep-OK (no thrash).
			phaseOf := func(name string) string {
				if activity == nil {
					return ""
				}
				return activity.Get(name).Phase
			}
			return HasOpenMissionForIdle(d, hooks.MissionOpen,
				CountWorkChildren(defs, d.Name),
				CountEngagedWorkChildren(defs, d.Name, phaseOf, hooks.MissionOpen))
		},
	})

	delivered, maxed := 0, 0
	for _, r := range reps {
		switch {
		case r.Delivered:
			delivered++
			s.logLifecycle(compIdleNudge, "idle_pressure", "ok", map[string]any{
				"agent": r.Name, "reason": r.Reason, "kind": string(r.Kind),
			})
		case r.Error != "":
			s.logLifecycle(compIdleNudge, "idle_pressure", "error", map[string]any{
				"agent": r.Name, "reason": r.Reason, "err": r.Error,
			})
		case r.Action == IdleNudgeMaxed:
			maxed++
			if hooks.OnMaxed != nil {
				hooks.OnMaxed(r)
			}
		}
	}
	if delivered > 0 || maxed > 0 {
		slog.Info("idle pressure sweep",
			"delivered", delivered, "maxed", maxed, "evaluated", len(reps))
	}
	return reps
}

// runFleetRecoverSweep evaluates open-mission workers for stuck-busy or
// terminal failure re-pressure (🎯T236). Fire-and-forget deliver.
func (s *Server) runFleetRecoverSweep(postRestart bool) {
	if s == nil || s.registry == nil {
		return
	}
	overseer := s.overseerName()
	if overseer == "" {
		overseer = "jevons"
	}
	s.mu.Lock()
	activity := s.idleActivity
	ledger := s.idleNudgeLedger
	s.mu.Unlock()
	if activity == nil {
		activity = NewIdleActivityTracker()
		s.mu.Lock()
		s.idleActivity = activity
		s.mu.Unlock()
	}

	push := func(target string, interrupt bool, event, text string) error {
		msg := formatIdleNudgeWire(event, text)
		_, err := s.sendDaemonComposed(target, msg, interrupt)
		return err
	}
	interruptFn := func(name string) error {
		proc := s.registry.Get(name)
		if proc == nil || !proc.Alive() {
			return fmt.Errorf("not running")
		}
		return proc.Interrupt()
	}

	reps := SweepFleetRecover(FleetRecoverSweepArgs{
		Reg:          s.registry,
		Activity:     activity,
		Ledger:       ledger,
		Push:         push,
		Interrupt:    interruptFn,
		Now:          time.Now(),
		OverseerName: overseer,
		StuckTimeout: DefaultFleetStuckTimeout,
		BriefPresent: func(name string) bool {
			s.mu.Lock()
			defer s.mu.Unlock()
			return s.fleetBriefed != nil && s.fleetBriefed[name]
		},
		MarkBriefed: func(name string) {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.fleetBriefed == nil {
				s.fleetBriefed = map[string]bool{}
			}
			s.fleetBriefed[name] = true
		},
	})
	delivered := 0
	for _, r := range reps {
		if r.Delivered {
			delivered++
			s.logLifecycle(compFleetRecover, string(r.Action), "ok", map[string]any{
				"agent":         r.Name,
				"reason":        r.Reason,
				"failure_class": string(r.FailureClass),
				"kind":          string(r.Kind),
				"interrupted":   r.Interrupted,
				"post_restart":  postRestart,
			})
		} else if r.Error != "" {
			s.logLifecycle(compFleetRecover, string(r.Action), "error", map[string]any{
				"agent":         r.Name,
				"reason":        r.Reason,
				"failure_class": string(r.FailureClass),
				"err":           r.Error,
			})
		}
	}
	if delivered > 0 {
		slog.Info("fleet recover sweep",
			"summary", FormatFleetRecoverSummary(reps),
			"post_restart", postRestart,
		)
	}
}

// emitWorkerIdleToParent fires worker-idle to the worker's parent PO when
// policy allows (open mission, debounce). Fire-and-forget; queues if PO busy.
//
// prevPhase/nextPhase are the observed transition, passed through so the
// decision goes through ShouldEmitWorkerIdle — which is where 🎯T414 intent is
// read. This notification instructs the parent to continue, re-brief, or
// restart the worker, so a parked, provider-blocked, or already-reaped agent
// must not be named by it (🎯T413).
func (s *Server) emitWorkerIdleToParent(name, prevPhase, nextPhase string) {
	if s == nil || s.registry == nil || name == "" {
		return
	}
	def := s.registry.Def(name)
	if def == nil {
		return
	}
	// 🎯T244: unbound POs with zero children skip emit.
	// 🎯T330: PO/boss with engaged implementers is sleep-OK — no parent thrash.
	defs := s.registry.List()
	workKids := CountWorkChildren(defs, name)
	s.mu.Lock()
	activity := s.idleActivity
	s.mu.Unlock()
	phaseOf := func(child string) string {
		if activity == nil {
			return ""
		}
		return activity.Get(child).Phase
	}
	// 🎯T451: this path used to pass nil for MissionOpen, so the ledger was
	// never consulted on the edge that actually generates the event — a worker
	// bound to an achieved or owner-parked target still woke its PO every time
	// its turn ended. The idle-pressure sweep has read the hook since 🎯T316;
	// the two classifiers disagreeing about what "open mission" means is its
	// own defect. Memoized because the hook re-reads and re-parses the ledger
	// per call and the engaged-child count asks about every child.
	missionOpen := memoMissionOpen(s.missionOpenHook())
	engagedKids := CountEngagedWorkChildren(defs, name, phaseOf, missionOpen)
	intent := s.fleetIntent()
	openMission := HasOpenMissionForIdle(*def, missionOpen, workKids, engagedKids)
	if !ShouldEmitWorkerIdle(prevPhase, nextPhase, def.Purpose, openMission,
		intent.FleetState(), intent.AgentState(name)) {
		if d := intent.Allow(name, fleetintent.ControlNotifyIdle); !d.Allow {
			slog.Info("worker-idle notification withheld — intent says do not run",
				"agent", name, "intent", string(d.Blocking), "reason", d.Reason)
		}
		return
	}
	overseer := s.overseerName()
	parent := ResolveEventParent(*def, defaultProductPOName, overseer)
	if parent == "" || parent == name {
		return
	}

	now := time.Now()
	s.mu.Lock()
	if s.idleEventLast == nil {
		s.idleEventLast = map[string]time.Time{}
	}
	if last, ok := s.idleEventLast[name]; ok && now.Sub(last) < DefaultWorkerIdleDebounce {
		s.mu.Unlock()
		return
	}
	s.idleEventLast[name] = now
	s.mu.Unlock()

	ref := WorkerIdleRef{
		Name:     def.Name,
		Parent:   parent,
		TargetID: def.TargetID,
		Purpose:  def.Purpose,
		Phase:    "idle",
	}
	text := FormatWorkerIdleText(ref)

	// 🎯T392.2: four idle workers under one PO should cost one coordinator
	// turn, not four. The debounce above is per-worker; this is
	// per-recipient. Batching off (nil batcher) delivers immediately, which
	// is the pre-T392.2 behaviour.
	s.mu.Lock()
	batcher := s.wakeBatch
	s.mu.Unlock()
	if batcher != nil {
		s.mu.Lock()
		deliverNow := batcher.Add(wakebatch.Event{
			Recipient: parent, Kind: eventWorkerIdle, Subject: name,
			Detail: text, At: now,
		})
		queued := batcher.Pending(parent)
		s.mu.Unlock()
		if !deliverNow {
			slog.Info("worker-idle event batched",
				"worker", name, "parent", parent, "pending", queued)
			return
		}
	}

	msg := formatIdleNudgeWire(eventWorkerIdle, text)
	// Do not interrupt PO mid-turn — queue if busy (T111.1).
	res, err := s.sendDaemonComposed(parent, msg, false)
	if err != nil {
		slog.Warn("worker-idle event deliver failed",
			"worker", name, "parent", parent, "err", err)
		s.logLifecycle(compIdleNudge, "worker_idle", "error", map[string]any{
			"worker": name, "parent": parent, "err": err.Error(),
		})
		return
	}
	slog.Info("worker-idle event delivered",
		"worker", name, "parent", parent, "status", res.Status, "queued", res.Queued)
	s.logLifecycle(compIdleNudge, "worker_idle", "ok", map[string]any{
		"worker": name, "parent": parent, "status": res.Status, "queued": res.Queued,
	})
}

// NotifyDaemonRestarted sends restart recovery once per durable parent PO and
// to the overseer (cockpit) (🎯T171 + 🎯T328).
//
//  1. Parent POs always get daemon-restarted (reattached children + silent OK).
//  2. Overseer: when stateDir/chatlog yields a recoverable open owner
//     instruction, deliver owner-intent-resume (forces real turn; not
//     silent-idle-only). Otherwise daemon-restarted status path as before.
//
// Fire-and-forget; queues if busy. Does not short-resume workers — that is
// ResumeOpenMissionWorkers. Residual: no chatlog / no recoverable intent.
func (s *Server) NotifyDaemonRestarted(overseer, defaultPO, stateDir string) {
	if s == nil || s.registry == nil {
		return
	}
	if overseer == "" {
		overseer = "jevons"
	}
	if defaultPO == "" {
		defaultPO = defaultProductPOName
	}
	running := func(name string) bool {
		proc := s.registry.Get(name)
		return proc != nil && proc.Alive()
	}
	phaseOf := func(name string) string {
		s.mu.Lock()
		act := s.idleActivity
		s.mu.Unlock()
		if act == nil {
			return "idle"
		}
		if p := act.Get(name).Phase; p != "" {
			return p
		}
		return "idle"
	}
	byParent := CollectWorkChildrenWithPhase(s.registry.List(), running, phaseOf, defaultPO, overseer)
	targets := DaemonRestartEventTargets(byParent, overseer, defaultPO)
	allKids := FlattenWorkChildren(byParent)

	// 🎯T328: recover open owner instruction for overseer resume (chatlog I/O).
	openIntent := LoadOpenOwnerIntent(stateDir, overseer)
	if openIntent.Recoverable() {
		slog.Info("open owner intent recovered for post-restart resume",
			"overseer", overseer, "runes", utf8RuneCount(openIntent.Text),
			"source", openIntent.Source)
	} else {
		slog.Info("no recoverable open owner intent after restart",
			"overseer", overseer, "residual", openIntent.Residual)
	}

	for _, target := range targets {
		kids := byParent[target]
		if target == overseer {
			// Cockpit gets the full reattached fleet summary.
			kids = allKids
		}
		event := eventDaemonRestarted
		text := FormatDaemonRestartedText(target, kids)
		// Overseer only: when open owner work is recoverable, force resume turn.
		if target == overseer && openIntent.Recoverable() {
			event = eventOwnerIntentResume
			text = FormatOverseerOpenIntentResume(openIntent, target, kids)
		}
		msg := formatIdleNudgeWire(event, text)
		// Do not interrupt PO/overseer mid-turn — queue if busy.
		res, err := s.sendDaemonComposed(target, msg, false)
		if err != nil {
			slog.Warn("daemon restart resume event deliver failed",
				"target", target, "event", event, "workers", len(kids), "err", err)
			s.logLifecycle(compIdleNudge, event, "error", map[string]any{
				"target": target, "workers": len(kids), "err": err.Error(),
				"open_intent": openIntent.Recoverable(), "residual": openIntent.Residual,
			})
			continue
		}
		slog.Info("daemon restart resume event delivered",
			"target", target, "event", event, "workers", len(kids),
			"status", res.Status, "queued", res.Queued,
			"open_intent", openIntent.Recoverable(), "residual", openIntent.Residual)
		s.logLifecycle(compIdleNudge, event, "ok", map[string]any{
			"target": target, "workers": len(kids), "status": res.Status, "queued": res.Queued,
			"open_intent": openIntent.Recoverable(), "residual": openIntent.Residual,
		})
	}
	// 🎯T418: isolate and headless daemons have no cockpit nudge. Recover
	// accepted queues and pending handovers on the restart notify itself.
	s.SweepSendBacklogs()
	s.SweepHandovers()
	s.reportFleetMuteIfNeeded()
}

// utf8RuneCount is a tiny local helper for NotifyDaemonRestarted logs.
func utf8RuneCount(s string) int {
	return len([]rune(s))
}

// ResumeOpenMissionWorkers fire-and-forget short-resumes open-mission work
// agents after restart via the T207 brief-or-verify send path (🎯T171 path 2).
// Skips aside, overseer, PO/boss, deliberate-stop, design-gated, looks-finished.
// Interrupt only when the prompt is already in flight (stuck recovery).
func (s *Server) ResumeOpenMissionWorkers(overseer, stateDir string, activity *IdleActivityTracker) {
	if s == nil || s.registry == nil {
		return
	}
	if overseer == "" {
		overseer = "jevons"
	}
	if activity == nil {
		s.mu.Lock()
		activity = s.idleActivity
		s.mu.Unlock()
	}
	if activity == nil {
		activity = NewIdleActivityTracker()
	}
	// Reuse the actuator's ledger when StartIdleNudgeLoop already opened it —
	// two in-memory copies of the same file lose counts on write.
	s.mu.Lock()
	ledger := s.idleNudgeLedger
	s.mu.Unlock()
	if ledger == nil && stateDir != "" {
		if l, err := OpenIdleNudgeLedger(stateDir); err == nil {
			ledger = l
			s.mu.Lock()
			s.idleNudgeLedger = l
			s.mu.Unlock()
		} else {
			slog.Warn("open-mission resume: ledger open failed", "err", err)
		}
	}

	// Fire-and-forget: send path queues when busy; interrupt only on stuck in-flight.
	push := func(target, event, text string) error {
		msg := formatIdleNudgeWire(event, text)
		// interrupt=true: if prompt in flight after reattach, clear stuck turn (T171).
		// When idle/send succeeds first try, interrupt is never invoked.
		_, err := s.sendDaemonComposed(target, msg, true)
		return err
	}

	reps := SweepIdleNudges(IdleNudgeSweepArgs{
		Reg:          s.registry,
		Activity:     activity,
		Ledger:       ledger,
		Push:         push,
		Now:          time.Now(),
		PostRestart:  true,
		OverseerName: overseer,
		BriefPresent: func(name string) bool {
			s.mu.Lock()
			defer s.mu.Unlock()
			return s.fleetBriefed != nil && s.fleetBriefed[name]
		},
		MarkBriefed: func(name string) {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.fleetBriefed == nil {
				s.fleetBriefed = map[string]bool{}
			}
			s.fleetBriefed[name] = true
		},
		// 🎯T414: the restart reattach path is the first of the three
		// resurrections of 2026-08-10 — the daemon came back, found workers
		// with open missions and no process, and started them. Intent is the
		// only thing here that can tell a crash from a park.
		Intent: s.fleetIntent(),
		// DesignGated / LooksFinished residual: no frontier probe on restart path.
	})

	delivered, skipped := 0, 0
	for _, r := range reps {
		if r.Delivered {
			delivered++
		} else {
			skipped++
		}
	}
	slog.Info("open-mission post-restart resume sweep",
		"delivered", delivered, "skipped_or_other", skipped, "reports", len(reps))
	s.logLifecycle(compIdleNudge, "post_restart_resume", "ok", map[string]any{
		"delivered": delivered, "reports": len(reps),
	})
}
