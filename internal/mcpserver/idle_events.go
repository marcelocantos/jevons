// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/targetfile"
	"github.com/marcelocantos/jevons/internal/wakebatch"
)

// 🎯T171 dual-path post-restart recovery (builds on 🎯T207 event path):
//
//  1. daemon-restarted → each durable parent PO + overseer (reattached summary)
//  2. short fire-and-forget resume → open-mission work agents only (T207 brief-or-verify path)
//  3. worker-idle → parent PO only on working→idle transition (debounced; not a poll)
//
// Rejected: only-PO hope, only-blast-everyone. Mechanical floor (dead/stuck) stays T204/T85.

const (
	// DefaultWorkerIdleDebounce avoids double-firing on flappy end_turn edges.
	DefaultWorkerIdleDebounce = 15 * time.Second
	// DefaultDaemonRestartNotifyDelay lets StartAll + wire settle.
	DefaultDaemonRestartNotifyDelay = 12 * time.Second

	eventWorkerIdle      = "worker-idle"
	eventDaemonRestarted = "daemon-restarted"
	defaultProductPOName = "jevons-po"
)

// WorkerIdleRef is one work agent mentioned in an idle/restart event body.
// SilentResponsePrefix / IsSilentAgentResponse live in silent_response.go (single definition).
type WorkerIdleRef struct {
	Name     string
	Parent   string
	TargetID string
	Purpose  string
	Phase    string
	Status   string // process status hint: running | stopped
}

// ShouldEmitWorkerIdle is the hermetic gate for enter-idle events.
// nextPhase must be idle; prevPhase must have been working (real turn),
// not seed-idle or empty (daemon boot seed is not an "enter idle" edge).
//
// fleetIntent / intent are the 🎯T414 deliberate states. The notification this
// gate emits does not merely inform — it tells the parent to continue,
// re-brief, or restart the worker — so it is a control, and 🎯T413 is what
// happens when a control has no way to ask whether the agent is supposed to
// be running: jv-t370-auto had already been reaped by the product, and the
// notifier's stale fleet view instructed the PO to restart it anyway.
func ShouldEmitWorkerIdle(prevPhase, nextPhase, purpose string, openMission bool, fleetIntent, intent fleetintent.State) bool {
	purpose = strings.TrimSpace(purpose)
	if purpose == "" {
		purpose = claudia.PurposeWork
	}
	if purpose == claudia.PurposeOverseer || purpose == claudia.PurposeAside {
		return false
	}
	if d := fleetintent.Allows(fleetIntent, intent, fleetintent.ControlNotifyIdle); !d.Allow {
		return false
	}
	if !openMission {
		return false
	}
	next := strings.ToLower(strings.TrimSpace(nextPhase))
	prev := strings.ToLower(strings.TrimSpace(prevPhase))
	if next != "idle" {
		return false
	}
	// Only fire on working → idle (turn actually ended).
	return prev == "working"
}

// ResolveEventParent picks who receives the idle/restart signal.
// Prefer AgentDef.Parent; empty → defaultPO (jevons-po); never the worker itself.
func ResolveEventParent(def claudia.AgentDef, defaultPO, overseer string) string {
	p := strings.TrimSpace(def.Parent)
	if p == "" {
		p = strings.TrimSpace(defaultPO)
	}
	if p == "" {
		p = defaultProductPOName
	}
	if p == def.Name {
		// Self-parented oddity — escalate to overseer if different.
		if overseer != "" && overseer != def.Name {
			return overseer
		}
		return defaultProductPOName
	}
	return p
}

// FormatWorkerIdleText builds the fire-and-forget body for a parent PO
// when a worker transitions into idle with open mission.
func FormatWorkerIdleText(w WorkerIdleRef) string {
	var b strings.Builder
	b.WriteString("A work agent under you entered phase=idle after generating (turn ended) ")
	b.WriteString("while it still looks like open mission work.\n\n")
	fmt.Fprintf(&b, "Worker: %s\n", strings.TrimSpace(w.Name))
	if tid := strings.TrimSpace(strings.TrimPrefix(w.TargetID, "🎯")); tid != "" {
		fmt.Fprintf(&b, "Target: 🎯%s\n", tid)
	} else {
		b.WriteString("Target: (none bound — still treat as open work unless you know it is done)\n")
	}
	if p := strings.TrimSpace(w.Parent); p != "" {
		fmt.Fprintf(&b, "Parent: %s\n", p)
	}
	b.WriteString("\nThis is the same class as a CLI session that stops after interrupt/restart ")
	b.WriteString("and needs an explicit continue — not a backend outage.\n")
	b.WriteString("Act: continue / re-brief / restart that worker / reap if done / file 🎯 if product gap.\n")
	b.WriteString("Do not wait for the owner to babysit. Local master only (T104) unless Ship was opened.\n")
	b.WriteString(silentResponseInstruction)
	return b.String()
}

// FormatDaemonRestartedText builds one parent- or overseer-facing restart brief
// listing reattached work children (name, target_id, status/phase).
func FormatDaemonRestartedText(parent string, workers []WorkerIdleRef) string {
	var b strings.Builder
	b.WriteString("jevonsd restarted (or reattached). Control plane is back.\n\n")
	b.WriteString("Hint: reattached sessions may be phase=idle (status=running) until they ")
	b.WriteString("receive a turn — not a default 'restart your workers' (process often fine). ")
	b.WriteString("Same class as resuming a CLI after Ctrl+C: explicit continue/re-brief, not more waiting.\n\n")
	if parent != "" {
		fmt.Fprintf(&b, "Addressed to: %s\n", parent)
	}
	if len(workers) == 0 {
		b.WriteString("No running work children listed under you right now.\n")
	} else {
		b.WriteString("Reattached work agents (name, target, status):\n")
		for _, w := range workers {
			tid := strings.TrimSpace(strings.TrimPrefix(w.TargetID, "🎯"))
			st := strings.TrimSpace(w.Status)
			if st == "" {
				st = "running"
			}
			ph := strings.TrimSpace(w.Phase)
			if ph == "" {
				ph = "idle"
			}
			if tid != "" {
				fmt.Fprintf(&b, "  - %s | 🎯%s | status=%s phase=%s\n", w.Name, tid, st, ph)
			} else {
				fmt.Fprintf(&b, "  - %s | status=%s phase=%s\n", w.Name, st, ph)
			}
		}
	}
	b.WriteString("\nDual path (🎯T171): framework also short-resumes open-mission workers; ")
	b.WriteString("you replan/reap/reseed. Do not blast missionless/aside/deliberate-stop. ")
	b.WriteString("Local master only (T104).\n")
	b.WriteString(silentResponseInstruction)
	return b.String()
}

// DaemonRestartEventTargets returns who should receive daemon-restarted:
// each parent key in byParent (durable POs / bosses) plus overseer when distinct.
// Pure helper for hermetic emit-target oracles (🎯T171).
func DaemonRestartEventTargets(byParent map[string][]WorkerIdleRef, overseer, defaultPO string) []string {
	if overseer == "" {
		overseer = "jevons"
	}
	if defaultPO == "" {
		defaultPO = defaultProductPOName
	}
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	if len(byParent) == 0 {
		add(defaultPO)
	} else {
		for parent := range byParent {
			// Parent keys that are the overseer still count if they own work
			// children; we always add overseer once below for cockpit.
			if parent == overseer {
				continue // overseer added explicitly as cockpit recipient
			}
			add(parent)
		}
		if len(out) == 0 {
			add(defaultPO)
		}
	}
	add(overseer) // always: cockpit dual-path (1)
	return out
}

// EligibleOpenMissionResume is the pure gate for post-restart short resume
// (🎯T171 path 2). True only for open-mission work implementers — not aside,
// overseer, PO/boss coordinators, deliberate-stop, design-gated, or finished.
//
// Open mission: purpose=work AND process running AND (AutoStart OR bound
// target_id) AND not the durable PO/boss name heuristic.
//
// intent is the 🎯T414 snapshot. Note what this gate must keep doing: an
// agent whose intent is working and whose process died is exactly the case
// 🎯T171 short-resume and 🎯T208 outage re-pressure exist for, and intent must
// not suppress it. Intent only declines when someone deliberately said this
// agent should not be running.
func EligibleOpenMissionResume(d claudia.AgentDef, running, deliberateStop, designGated, looksFinished bool, intent fleetintent.Snapshot) bool {
	if deliberateStop || designGated || looksFinished {
		return false
	}
	if dec := intent.Allow(d.Name, fleetintent.ControlRevive); !dec.Allow {
		return false
	}
	if !running {
		return false
	}
	purpose := strings.TrimSpace(d.Purpose)
	if purpose == "" {
		purpose = claudia.PurposeWork
	}
	if purpose == claudia.PurposeOverseer || purpose == claudia.PurposeAside {
		return false
	}
	if purpose != claudia.PurposeWork {
		return false
	}
	// POs/bosses get daemon-restarted (path 1), not implementer short-resume.
	if looksLikePOOrBoss(d.Name) {
		return false
	}
	tid := strings.TrimSpace(d.TargetID)
	if tid == "" && !d.AutoStart {
		return false // missionless non-durable: do not blast
	}
	// AutoStart and/or bound target_id.
	return d.AutoStart || tid != ""
}

// CollectWorkChildren groups running purpose=work agents by ResolveEventParent.
// phaseByName is optional (name → phase); default phase is idle after reattach.
func CollectWorkChildren(defs []claudia.AgentDef, running func(name string) bool, defaultPO, overseer string) map[string][]WorkerIdleRef {
	return CollectWorkChildrenWithPhase(defs, running, nil, defaultPO, overseer)
}

// CollectWorkChildrenWithPhase is CollectWorkChildren with optional phase map.
func CollectWorkChildrenWithPhase(defs []claudia.AgentDef, running func(name string) bool, phaseByName func(name string) string, defaultPO, overseer string) map[string][]WorkerIdleRef {
	out := map[string][]WorkerIdleRef{}
	if running == nil {
		running = func(string) bool { return false }
	}
	for _, d := range defs {
		if d.Name == "" || d.Name == overseer {
			continue
		}
		purpose := d.Purpose
		if purpose == "" {
			purpose = claudia.PurposeWork
		}
		if purpose != claudia.PurposeWork {
			continue
		}
		if !running(d.Name) {
			continue
		}
		parent := ResolveEventParent(d, defaultPO, overseer)
		phase := "idle"
		if phaseByName != nil {
			if p := strings.TrimSpace(phaseByName(d.Name)); p != "" {
				phase = p
			}
		}
		ref := WorkerIdleRef{
			Name:     d.Name,
			Parent:   parent,
			TargetID: d.TargetID,
			Purpose:  purpose,
			Phase:    phase,
			Status:   "running",
		}
		out[parent] = append(out[parent], ref)
	}
	return out
}

// FlattenWorkChildren returns a de-duplicated list of all work refs in byParent.
func FlattenWorkChildren(byParent map[string][]WorkerIdleRef) []WorkerIdleRef {
	seen := map[string]bool{}
	var out []WorkerIdleRef
	for _, kids := range byParent {
		for _, w := range kids {
			if w.Name == "" || seen[w.Name] {
				continue
			}
			seen[w.Name] = true
			out = append(out, w)
		}
	}
	return out
}

// CountWorkChildren returns how many purpose=work agents list parentName as Parent.
// Used by the enter-idle open-mission gate (🎯T244) so unbound POs with zero
// work children are not treated as open mission.
func CountWorkChildren(defs []claudia.AgentDef, parentName string) int {
	parentName = strings.TrimSpace(parentName)
	if parentName == "" {
		return 0
	}
	n := 0
	for _, c := range defs {
		if strings.TrimSpace(c.Parent) != parentName {
			continue
		}
		purpose := strings.TrimSpace(c.Purpose)
		if purpose == "" {
			purpose = claudia.PurposeWork
		}
		if purpose == claudia.PurposeWork {
			n++
		}
	}
	return n
}

// IsEngagedWorkChild reports whether a purpose=work child is engaged on open
// mission work: target_id bound (and still open via missionOpen if set)
// and/or phase=working (mid-turn WIP). 🎯T330.
//
// phaseByName and missionOpen may be nil. Empty phase does not count as WIP;
// closed missions (missionOpen false) do not count as bound-open.
func IsEngagedWorkChild(c claudia.AgentDef, phase string, missionOpen func(targetID string) bool) bool {
	purpose := strings.TrimSpace(c.Purpose)
	if purpose == "" {
		purpose = claudia.PurposeWork
	}
	if purpose != claudia.PurposeWork {
		return false
	}
	tid := strings.TrimSpace(c.TargetID)
	if tid != "" {
		if missionOpen != nil {
			return missionOpen(tid)
		}
		return true
	}
	// Open mission WIP: mid-turn implementer (the T329 failure mode).
	return strings.ToLower(strings.TrimSpace(phase)) == "working"
}

// CountEngagedWorkChildren counts purpose=work children of parentName that are
// engaged (bound open target and/or phase=working). 🎯T330.
// phaseByName and missionOpen may be nil.
func CountEngagedWorkChildren(defs []claudia.AgentDef, parentName string, phaseByName func(string) string, missionOpen func(string) bool) int {
	parentName = strings.TrimSpace(parentName)
	if parentName == "" {
		return 0
	}
	n := 0
	for _, c := range defs {
		if strings.TrimSpace(c.Parent) != parentName {
			continue
		}
		phase := ""
		if phaseByName != nil {
			phase = phaseByName(c.Name)
		}
		if IsEngagedWorkChild(c, phase, missionOpen) {
			n++
		}
	}
	return n
}

// SuppressParentIdleThrash is the pure 🎯T330 gate: a PO/boss with ≥1 engaged
// work children must not be re-continue-thrashed by idle-pressure or
// worker-idle open-mission classification (sleep-OK while implementers work).
func SuppressParentIdleThrash(name string, engagedChildCount int) bool {
	return looksLikePOOrBoss(name) && engagedChildCount > 0
}

// HasOpenMissionForIdle is the default open-mission heuristic for enter-idle
// and idle-pressure eligibility.
//
// purpose=work with a bound TargetID is open (callers may tighten with
// missionOpen). Unbound implementers stay open so the parent PO can reap.
//
// 🎯T244: unbound PO/boss-shaped agents with zero work children are NOT open
// mission — long-lived standing idle must not thrash the overseer.
//
// 🎯T330: PO/boss with ≥1 engaged work children is sleep-OK — not open mission
// for parent re-continue thrash; children own progress. Residual: orphan PO
// idle with zero children still uses T244; open-frontier kick remains T325.1.
// engagedChildCount 0 preserves pre-T330 behaviour when callers omit it.
func HasOpenMissionForIdle(d claudia.AgentDef, missionOpen func(targetID string) bool, workChildCount, engagedChildCount int) bool {
	purpose := d.Purpose
	if purpose == "" {
		purpose = claudia.PurposeWork
	}
	if purpose != claudia.PurposeWork {
		return false
	}
	// 🎯T330: supervising engaged implementers ⇒ sleep-OK, no parent thrash.
	if SuppressParentIdleThrash(d.Name, engagedChildCount) {
		return false
	}
	tid := strings.TrimSpace(d.TargetID)
	if tid == "" {
		// 🎯T244: unbound PO/boss with no work children = standing idle, not open mission.
		if looksLikePOOrBoss(d.Name) && workChildCount <= 0 {
			return false
		}
		return true // unbound implementer, or PO with only unengaged kids (reap)
	}
	if missionOpen != nil {
		return missionOpen(tid)
	}
	return true
}

// Wake batching (🎯T392.2).
//
// DefaultWakeBatchWindow is how long machine-generated events accumulate
// before one digest is delivered. Three minutes is chosen against the
// idle-nudge threshold (2m) and pressure interval (1m): long enough that a
// sweep's worth of idle workers lands in one digest, short enough that a
// genuinely stuck fleet is surfaced in single-digit minutes.
const DefaultWakeBatchWindow = 3 * time.Minute

// SetWakeBatchWindow enables coalescing. Zero disables it, restoring
// immediate per-event delivery.
func (s *Server) SetWakeBatchWindow(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d <= 0 {
		s.wakeBatch = nil
		return
	}
	s.wakeBatch = wakebatch.New(d)
}

// NewLedgerMissionOpen builds the satisfaction seam the daemon installs as
// IdlePressureHooks.MissionOpen: a target is still open work when the ledger
// nearest an agent bound to it says so.
//
// It lives here rather than as a closure in main.go so the 🎯T451 oracle
// exercises the shipped classifier instead of a copy of it — a fixture that
// re-implements the rule it is checking proves only that the fixture agrees
// with itself.
//
// Residual, deliberately: a target no registered agent is bound to, an
// unreadable ledger, and an unknown row all stay open. Silence is the
// dangerous answer here — it stops a real worker being nudged — so it is only
// returned on a positive reading.
func NewLedgerMissionOpen(list func() []claudia.AgentDef) func(targetID string) bool {
	return func(targetID string) bool {
		tid := strings.TrimSpace(strings.TrimPrefix(targetID, "🎯"))
		if tid == "" || list == nil {
			return true
		}
		for _, d := range list() {
			if strings.TrimSpace(strings.TrimPrefix(d.TargetID, "🎯")) != tid {
				continue
			}
			// 🎯T451: status alone cannot answer this. An owner-parked target
			// keeps whatever status it had — 🎯T370 is `converging`, owned by
			// marcelo, and no agent may finish it — so owner assignment closes
			// the mission the same way `achieved` does.
			return targetfile.MissionOpenFromCwd(d.WorkDir, tid)
		}
		return true
	}
}

// FilterIdleEventsForLiveAgents drops worker-idle events whose subject has
// left the registry between enqueue and delivery (🎯T451).
//
// An idle event is emitted the moment a worker's turn ends — which is the
// same moment it delivers a terminal report, and 🎯T165 stops and Removes it
// on the strength of that report. The event is then held for a batching
// window (three minutes by default, 🎯T392.2), so the ordinary end of a
// successful worker is a digest that names an agent the reader can no longer
// find, send to, or stand down. bullseye-po was told four times about two such
// agents; a stand-down send came back `agent "bs-t70-release-probe-ci" is not
// running`.
//
// The cost is not the noise: the correct response to those lines is to do
// nothing, and the event's own text tells the reader not to do nothing. So it
// spends a coordinator turn to establish that nothing is wrong.
//
// present may be nil (no registry to ask), in which case nothing is dropped —
// an event is only withheld on a positive answer that the agent is gone.
// Non-agent event kinds pass through untouched.
func FilterIdleEventsForLiveAgents(evs []wakebatch.Event, present func(name string) bool) []wakebatch.Event {
	if present == nil {
		return evs
	}
	out := evs[:0:0]
	for _, ev := range evs {
		if ev.Kind == eventWorkerIdle && strings.TrimSpace(ev.Subject) != "" &&
			!present(ev.Subject) {
			continue
		}
		out = append(out, ev)
	}
	return out
}

// FlushWakeBatches delivers every digest whose window has elapsed. Returns
// how many recipients were woken — far fewer than the events they carry,
// which is the entire point.
//
// Delivery failures drop the digest rather than retrying: these are idle
// notices, and the next sweep re-observes the same idle workers anyway. A
// retry queue here would be machinery for events that regenerate on their
// own.
func (s *Server) FlushWakeBatches() int {
	return s.flushWakeBatches(nil)
}

// flushWakeBatches is FlushWakeBatches with an injectable sender so the
// 🎯T451 oracle can observe what a digest would have named without launching
// agent processes. send nil = the live deliver path.
func (s *Server) flushWakeBatches(send func(recipient, text string) error) int {
	s.mu.Lock()
	batcher := s.wakeBatch
	s.mu.Unlock()
	if batcher == nil {
		return 0
	}
	if send == nil {
		send = func(recipient, text string) error {
			res, err := s.sendToAgent(recipient, formatIdleNudgeWire(eventWorkerIdle, text), false)
			if err == nil {
				slog.Info("wake digest sent", "recipient", recipient,
					"status", res.Status, "queued", res.Queued)
			}
			return err
		}
	}
	// 🎯T451: registry membership is re-read at delivery, not at enqueue. The
	// window between them is exactly where an agent finishes and is reaped.
	var present func(string) bool
	if s.registry != nil {
		present = func(name string) bool { return s.registry.Def(name) != nil }
	}
	now := time.Now()
	s.mu.Lock()
	due := batcher.Due(now)
	pending := make(map[string][]wakebatch.Event, len(due))
	for _, recipient := range due {
		pending[recipient] = batcher.Take(recipient)
	}
	s.mu.Unlock()

	woken := 0
	for _, recipient := range due {
		evs := pending[recipient]
		if kept := FilterIdleEventsForLiveAgents(evs, present); len(kept) != len(evs) {
			slog.Info("wake digest dropped events for deregistered agents",
				"recipient", recipient, "dropped", len(evs)-len(kept), "kept", len(kept))
			evs = kept
		}
		text := wakebatch.FormatDigest(evs)
		if text == "" {
			continue
		}
		err := send(recipient, text)
		if err != nil {
			slog.Warn("wake digest deliver failed",
				"recipient", recipient, "events", len(evs), "err", err)
			continue
		}
		woken++
		slog.Info("wake digest delivered",
			"recipient", recipient, "events", len(evs),
			"turns_saved", len(evs)-1)
		s.logLifecycle(compIdleNudge, "wake_digest", "ok", map[string]any{
			"recipient": recipient, "events": len(evs), "turns_saved": len(evs) - 1,
		})
	}
	return woken
}
