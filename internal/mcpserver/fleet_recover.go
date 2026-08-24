// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agenterr"
)

// 🎯T236: after provider outage or failed turns, re-pressure open-mission
// fleet workers without owner "continue".
//
// Covers:
//  1. Aged prompt-in-flight with no ACP progress (stuck-busy for workers,
//     not overseer-only — cockpit T204 remains for overseer).
//  2. Terminal error/empty end_turn with a structured failure class from
//     agenterr (T237 surface) → continue/re-brief the agent.
//
// Residual: healthy multi-hour tool turns with progress heartbeats are
// not false-unstuck (SinceProgress < StuckTimeout).

// fleetRecoverWorthy: re-pressure for transient backend/rate_limit and
// unknown residual failures; auth/client_bug fail closed (T237).
func fleetRecoverWorthy(c agenterr.Class) bool {
	// TransientBackend = backend_unavailable | rate_limit; also re-pressure unknown.
	if agenterr.TransientBackend(c) || c == agenterr.ClassUnknown {
		return true
	}
	return false
}

const (
	// DefaultFleetStuckTimeout matches overseer stuck-busy (T204).
	DefaultFleetStuckTimeout = 90 * time.Second
	// DefaultFleetRecoverMax caps auto recovers per agent (same floor as idle nudge).
	DefaultFleetRecoverMax = 8
	// compFleetRecover is the lifecycle log component.
	compFleetRecover = "fleet_recover"
)

// DefaultFleetRecoverBackoffs is minimum wait after recover N before N+1.
var DefaultFleetRecoverBackoffs = []time.Duration{
	2 * time.Minute,
	5 * time.Minute,
	15 * time.Minute,
}

// FleetRecoverAction is the pure-policy decision for one worker observation.
type FleetRecoverAction string

const (
	FleetRecoverSkip    FleetRecoverAction = "skip"
	FleetRecoverUnstick FleetRecoverAction = "unstick" // interrupt + re-brief
	FleetRecoverRebrief FleetRecoverAction = "rebrief" // deliver continue/brief only
	FleetRecoverMaxed   FleetRecoverAction = "maxed"
)

// FleetRecoverObs is a pure snapshot — no I/O.
type FleetRecoverObs struct {
	Name           string
	Purpose        string
	ProcessRunning bool
	DeliberateStop bool
	HasOpenMission bool
	DesignGated    bool
	LooksFinished  bool
	// PromptInFlight from claudia (Grok ACP); false when unknown.
	PromptInFlight bool
	// SinceProgress is time since last ACP event; 0 when never.
	SinceProgress time.Duration
	// NeverProgressed is true when no ACP event was ever observed while
	// something claims work (prompt in flight / needs recover).
	NeverProgressed bool
	Phase           string
	// FailureClass is the structured class from T237/agenterr (last terminal
	// failure). ClassNone when no failure recorded.
	FailureClass agenterr.Class
	// NeedsRecover is set when a terminal error/empty end_turn was observed
	// and not yet cleared by a successful recover deliver.
	NeedsRecover bool
	// TerminalEmpty: last terminal stop had empty/whitespace text.
	TerminalEmpty  bool
	StuckTimeout   time.Duration // 0 → DefaultFleetStuckTimeout
	BriefPresent   bool
	RecoverCount   int
	SinceLastRecov time.Duration
	EverRecovered  bool
	MaxRecovers    int // 0 → DefaultFleetRecoverMax
	Backoffs       []time.Duration
	// SessionReminted: this boot minted a new session_id (🎯T545.1).
	SessionReminted bool
}

// ClassifyFleetRecover decides skip | unstick | rebrief | maxed for one agent.
// Hermetic oracle surface for 🎯T236.
func ClassifyFleetRecover(o FleetRecoverObs) (FleetRecoverAction, string) {
	purpose := strings.TrimSpace(o.Purpose)
	if purpose == "" {
		purpose = claudia.PurposeWork
	}
	if purpose == claudia.PurposeOverseer || purpose == claudia.PurposeAside {
		return FleetRecoverSkip, "not_work_purpose"
	}
	if o.SessionReminted {
		return FleetRecoverSkip, "bounce_remint"
	}
	if o.LooksFinished {
		return FleetRecoverSkip, "achieved_should_reap"
	}
	if o.DesignGated {
		return FleetRecoverSkip, "design_gated"
	}
	if o.DeliberateStop {
		return FleetRecoverSkip, "deliberate_stop"
	}
	if !o.HasOpenMission {
		return FleetRecoverSkip, "no_open_mission"
	}
	if !o.ProcessRunning {
		return FleetRecoverSkip, "not_running"
	}

	max := o.MaxRecovers
	if max <= 0 {
		max = DefaultFleetRecoverMax
	}
	if o.RecoverCount >= max {
		return FleetRecoverMaxed, "max_recovers"
	}

	// Backoff after a prior recover (prevents thrash).
	if o.EverRecovered {
		need := NextNudgeBackoff(o.RecoverCount, o.Backoffs)
		if len(o.Backoffs) == 0 {
			need = NextNudgeBackoff(o.RecoverCount, DefaultFleetRecoverBackoffs)
		}
		if o.SinceLastRecov < need {
			return FleetRecoverSkip, "backoff"
		}
	}

	stuckTO := o.StuckTimeout
	if stuckTO <= 0 {
		stuckTO = DefaultFleetStuckTimeout
	}
	since := o.SinceProgress
	if o.NeverProgressed && (o.PromptInFlight || o.NeedsRecover) {
		since = stuckTO + time.Second
	}

	// 1) Stuck-busy: prompt in flight, no ACP progress beyond timeout.
	// Healthy long tool turns keep SinceProgress fresh via heartbeats.
	if o.PromptInFlight && since >= stuckTO {
		return FleetRecoverUnstick, "stuck_busy"
	}

	// 2) Terminal failure / empty end_turn → re-brief without owner continue.
	// Only when the structured class is re-pressure-worthy (T237 TransientBackend)
	// or the terminal was empty (silent failure / empty Internal path).
	if o.NeedsRecover && !o.PromptInFlight {
		if o.TerminalEmpty {
			return FleetRecoverRebrief, "terminal_empty"
		}
		if fleetRecoverWorthy(o.FailureClass) {
			return FleetRecoverRebrief, "terminal_failure:" + string(o.FailureClass)
		}
		if agenterr.IsFailure(o.FailureClass) {
			// Auth / client_bug: do not busy-loop; skip mechanical re-brief.
			return FleetRecoverSkip, "non_transient:" + string(o.FailureClass)
		}
		// NeedsRecover set without class (defensive) → still re-pressure.
		return FleetRecoverRebrief, "needs_recover"
	}

	// Working with fresh progress → healthy.
	phase := strings.ToLower(strings.TrimSpace(o.Phase))
	if phase == "working" && o.PromptInFlight && since < stuckTO {
		return FleetRecoverSkip, "in_progress"
	}
	if phase == "working" && !o.NeedsRecover {
		return FleetRecoverSkip, "in_progress"
	}

	return FleetRecoverSkip, "no_recover_signal"
}

// FleetRecoverReport is one agent evaluation outcome.
type FleetRecoverReport struct {
	Name         string
	Action       FleetRecoverAction
	Reason       string
	Kind         IdleNudgeKind
	FailureClass agenterr.Class
	Delivered    bool
	Interrupted  bool
	Error        string
}

// FleetRecoverPusher delivers a resume brief (production: sendToAgent path).
// interrupt=true when unsticking an in-flight prompt.
type FleetRecoverPusher func(target string, interrupt bool, event, text string) error

// FleetRecoverInterrupt cancels an in-flight prompt (optional separate from push).
type FleetRecoverInterrupt func(name string) error

// FleetRecoverSweepArgs configures one fleet evaluation.
type FleetRecoverSweepArgs struct {
	Reg          *claudia.Registry
	Activity     *IdleActivityTracker
	Ledger       *IdleNudgeLedger // reuse nudge ledger for backoff/max
	Push         FleetRecoverPusher
	Interrupt    FleetRecoverInterrupt // optional; if nil, push with interrupt=true
	Now          time.Time
	OverseerName string
	StuckTimeout time.Duration
	// PromptInFlight: name → in flight. Nil → reg.Get(name).PromptInFlight().
	PromptInFlight func(name string) bool
	// ProcessRunning optional override for hermetic tests.
	ProcessRunning     func(name string) bool
	BriefPresent       func(name string) bool
	MarkBriefed        func(name string)
	DesignGated        func(targetID string) bool
	MissionOpen        func(targetID string) bool
	LastTerminalReport func(name string) string
	MissionAcceptance  func(targetID string) string
	// SessionReminted is optional: name → this boot reminted session_id
	// (🎯T545.1). Nil = no remints.
	SessionReminted func(name string) bool
}

// SweepFleetRecover classifies every registered work agent and unsticks /
// re-briefs when policy says so (🎯T236).
func SweepFleetRecover(args FleetRecoverSweepArgs) []FleetRecoverReport {
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
	var out []FleetRecoverReport
	for _, d := range args.Reg.List() {
		if d.Name == "" || d.Name == overseer {
			continue
		}
		rep := evaluateAndMaybeRecover(d, args, now)
		if rep.Action == FleetRecoverSkip && rep.Reason == "not_work_purpose" {
			continue
		}
		out = append(out, rep)
	}
	return out
}

func evaluateAndMaybeRecover(d claudia.AgentDef, args FleetRecoverSweepArgs, now time.Time) FleetRecoverReport {
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
	deliberateStop := !running && !d.AutoStart

	act := IdleActivity{}
	if args.Activity != nil {
		act = args.Activity.Get(d.Name)
	}
	since := time.Duration(0)
	never := act.Updated.IsZero()
	if !never {
		since = now.Sub(act.Updated)
	}

	inFlight := false
	if args.PromptInFlight != nil {
		inFlight = args.PromptInFlight(d.Name)
	} else if proc := args.Reg.Get(d.Name); proc != nil {
		inFlight = proc.PromptInFlight()
	}

	count := 0
	var last time.Time
	if args.Ledger != nil {
		count, last = args.Ledger.Get(d.Name)
	}
	sinceRecov := time.Duration(0)
	ever := !last.IsZero()
	if ever {
		sinceRecov = now.Sub(last)
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
	} else if purpose == claudia.PurposeWork {
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

	obs := FleetRecoverObs{
		Name:            d.Name,
		Purpose:         purpose,
		ProcessRunning:  running,
		DeliberateStop:  deliberateStop,
		HasOpenMission:  hasMission,
		DesignGated:     designGated,
		LooksFinished:   looksFinished,
		PromptInFlight:  inFlight,
		SinceProgress:   since,
		NeverProgressed: never,
		Phase:           act.Phase,
		FailureClass:    act.FailureClass,
		NeedsRecover:    act.NeedsRecover,
		TerminalEmpty:   act.TerminalEmpty,
		StuckTimeout:    args.StuckTimeout,
		BriefPresent:    briefPresent,
		RecoverCount:    count,
		SinceLastRecov:  sinceRecov,
		EverRecovered:   ever,
		Backoffs:        DefaultFleetRecoverBackoffs,
		SessionReminted: args.SessionReminted != nil && args.SessionReminted(d.Name),
	}
	action, reason := ClassifyFleetRecover(obs)
	kind := ClassifyIdleNudgeKind(briefPresent)
	rep := FleetRecoverReport{
		Name:         d.Name,
		Action:       action,
		Reason:       reason,
		Kind:         kind,
		FailureClass: act.FailureClass,
	}
	if action != FleetRecoverUnstick && action != FleetRecoverRebrief {
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
		PostRestart: false,
		Kind:        kind,
	})
	// Prefix reason with recovery context so workers see outage recovery.
	if !strings.Contains(text, "RECOVER") {
		text = "RECOVER (provider/outage path 🎯T236) — " + text
	}
	event := "fleet-recover"
	if action == FleetRecoverUnstick {
		event = "fleet-recover-unstick"
	}

	// Unstick: interrupt first when a dedicated Interrupt is provided;
	// otherwise push with interrupt=true (send path cancels in-flight).
	doInterrupt := action == FleetRecoverUnstick
	if doInterrupt && args.Interrupt != nil {
		if err := args.Interrupt(d.Name); err != nil {
			slog.Warn("fleet recover interrupt failed",
				"agent", d.Name, "err", err, "failure_class", act.FailureClass)
			// Still attempt deliver — Cancel may have partially cleared.
		} else {
			rep.Interrupted = true
		}
		doInterrupt = false // already interrupted; push without re-interrupt race
	}

	if err := args.Push(d.Name, doInterrupt && action == FleetRecoverUnstick, event, text); err != nil {
		rep.Error = err.Error()
		slog.Warn("fleet recover deliver failed",
			"agent", d.Name, "action", action, "reason", reason,
			"failure_class", act.FailureClass, "err", err)
		return rep
	}
	rep.Delivered = true
	if action == FleetRecoverUnstick {
		rep.Interrupted = true
	}
	if kind == IdleNudgeKindFullBrief && args.MarkBriefed != nil {
		args.MarkBriefed(d.Name)
	}
	if args.Ledger != nil {
		if err := args.Ledger.Record(d.Name, now); err != nil {
			slog.Warn("fleet recover ledger record failed", "agent", d.Name, "err", err)
		}
	}
	// Clear NeedsRecover and reset idle clock (phase stays idle until ACP).
	if args.Activity != nil {
		args.Activity.ClearRecover(d.Name, now)
	}
	slog.Info("fleet recover delivered",
		"agent", d.Name,
		"action", action,
		"reason", reason,
		"kind", kind,
		"failure_class", act.FailureClass,
		"interrupted", rep.Interrupted,
	)
	return rep
}

// FormatFleetRecoverSummary is a one-line log/oracle summary.
func FormatFleetRecoverSummary(reps []FleetRecoverReport) string {
	var delivered, skipped, failed int
	for _, r := range reps {
		if r.Delivered {
			delivered++
		} else if r.Error != "" {
			failed++
		} else {
			skipped++
		}
	}
	return fmt.Sprintf("fleet_recover delivered=%d skipped=%d failed=%d total=%d",
		delivered, skipped, failed, len(reps))
}

// --- activity: terminal failure tracking ---

// NoteTerminalOutcome records structured failure class + recover need after
// an end_turn (or equivalent). Pure class via agenterr (T237); no prose scrape
// in the recovery decision path beyond ClassifyText fixtures.
//
// 🎯T454: also latches RefusalHold from whole-turn classification so the
// impatience ladder cannot close on a spend-limit / auth wall as if the
// agent had resumed work.
func (t *IdleActivityTracker) NoteTerminalOutcome(name, terminalText string) {
	if t == nil || name == "" {
		return
	}
	text := strings.TrimSpace(terminalText)
	empty := text == ""
	class := agenterr.ClassifyText(text)
	// Empty terminal always needs recover for open-mission (caller filters).
	// Transient / unknown failures need recover; auth/client fail closed.
	needs := empty || fleetRecoverWorthy(class)
	kind := agenterr.ClassifyTurnOutput(text)

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.by == nil {
		t.by = make(map[string]IdleActivity)
	}
	prev := t.by[name]
	prev.Phase = "idle"
	prev.Updated = t.clock()
	prev.FailureClass = class
	prev.TerminalEmpty = empty
	prev.NeedsRecover = needs
	prev.LastTerminal = truncate(text, 400)
	switch kind {
	case agenterr.TurnRefusalOnly:
		prev.RefusalHold = true
	case agenterr.TurnSubstantive:
		prev.RefusalHold = false
		prev.SubstantivePulse = true
	}
	// TurnEmpty leaves RefusalHold unchanged — an empty terminal after a
	// wall is not evidence the account recovered.
	t.by[name] = prev
}

// ConsumeTurnPulses returns and clears the one-shot substantive pulse for
// name (🎯T454). Safe under the tracker lock.
func (t *IdleActivityTracker) ConsumeTurnPulses(name string) (substantive bool) {
	if t == nil || name == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	prev, ok := t.by[name]
	if !ok || !prev.SubstantivePulse {
		return false
	}
	prev.SubstantivePulse = false
	t.by[name] = prev
	return true
}

func truncateTerminal(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n]
}

// ClearRecover clears the recover latch after a successful deliver.
func (t *IdleActivityTracker) ClearRecover(name string, at time.Time) {
	if t == nil || name == "" {
		return
	}
	if at.IsZero() {
		at = t.clock()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.by == nil {
		t.by = make(map[string]IdleActivity)
	}
	prev := t.by[name]
	prev.Phase = "idle"
	prev.Updated = at
	prev.NeedsRecover = false
	prev.TerminalEmpty = false
	// Keep FailureClass for structured logs on subsequent cycles.
	t.by[name] = prev
}

// TouchProgress updates the last-progress clock without changing phase
// (ACP heartbeat so stuck-busy does not false-unstick healthy turns).
func (t *IdleActivityTracker) TouchProgress(name string) {
	if t == nil || name == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.by == nil {
		t.by = make(map[string]IdleActivity)
	}
	prev := t.by[name]
	prev.Updated = t.clock()
	if prev.Phase == "" {
		prev.Phase = "working"
	}
	t.by[name] = prev
}
