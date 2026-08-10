// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Convergence exhaustion (🎯T415).
//
// The impatience ladder already gives up: ClassifyIdleNudge returns
// IdleNudgeMaxed after DefaultIdleNudgeMax attempts, and
// IdlePressureHooks declares OnMaxed for "escalation/noise ladder for an
// agent that exhausted its nudge budget". Nothing ever set OnMaxed, so
// exhaustion incremented a counter and the agent was abandoned in
// silence. This is that hook's implementation.
//
// TWO INDEPENDENT PATHS, NEVER A STATE MACHINE.
//
//	always     — deterministic notice to the owner's journal
//	best-effort — a disposable recovery agent diagnoses
//
// The notice is NOT a step in a protocol. It is not "spawn a recovery
// agent, and notify if that fails", because that reintroduces the
// dependency the whole design exists to remove. It is "notify, and
// separately try to fix". The repair path may fail, may fail to spawn at
// all — which is the COMMON case exactly when it is most needed, since
// quota exhaustion and a daemon that cannot launch agents are precisely
// the faults it would be diagnosing — and nothing depends on it.
//
// The stuck agent is deliberately left stuck. Recovery destroys the
// evidence, and a fleet that silently repairs what it cannot explain
// accumulates unexplained repairs: one lost-session bug survived in three
// separate code paths for a day because each site quietly worked around
// it (🎯T409).

// OwnerNotifier writes a deterministic notice to the owner's journal.
// Implemented by *server.Server; nil leaves exhaustion logged but
// unreported, which is the pre-🎯T415 behaviour and is reported loudly at
// wiring time rather than discovered later.
type OwnerNotifier interface {
	NotifyOwner(subject, kind, text string) bool
}

// exhaustionState dedups repeated exhaustion of the same agent.
type exhaustionState struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

// DefaultExhaustionRenotify is how long before the same agent failing the
// same way is worth telling the owner about again. A stuck agent stays
// stuck; repeating that every sweep would train the owner to ignore the
// surface, which is the failure mode an always-notify design has to
// respect.
const DefaultExhaustionRenotify = 6 * time.Hour

func (e *exhaustionState) shouldNotify(name string, now time.Time, within time.Duration) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.seen == nil {
		e.seen = map[string]time.Time{}
	}
	if last, ok := e.seen[name]; ok && now.Sub(last) < within {
		return false
	}
	e.seen[name] = now
	return true
}

// FormatExhaustionNotice is the owner-facing text. Pure, so the wording
// is testable without a fleet.
func FormatExhaustionNotice(rep IdleNudgeReport, attempts int) string {
	s := fmt.Sprintf(
		"⚠️ %s is stuck and convergence has given up after %d attempts.",
		rep.Name, attempts)
	if rep.Reason != "" {
		s += " Last verdict: " + rep.Reason + "."
	}
	if rep.Error != "" {
		s += " Last error: " + rep.Error + "."
	}
	s += "\n\nIt has been left stuck on purpose so the cause can be diagnosed" +
		" rather than papered over. A recovery agent has been asked to look" +
		" into it; if it does not report back, that is itself a finding."
	return s
}

// OnConvergenceExhausted is the OnMaxed implementation.
func (s *Server) OnConvergenceExhausted(rep IdleNudgeReport) {
	if s == nil {
		return
	}
	now := time.Now()
	if !s.exhaustion.shouldNotify(rep.Name, now, DefaultExhaustionRenotify) {
		slog.Info("convergence exhausted (already reported)", "agent", rep.Name)
		return
	}

	// ── Path 1: always, deterministic, load-bearing ──────────────────
	text := FormatExhaustionNotice(rep, DefaultIdleNudgeMax)
	if s.ownerNotifier != nil {
		s.ownerNotifier.NotifyOwner(rep.Name, "convergence-exhausted", text)
	} else {
		// Loud: an unreported exhaustion is the exact condition this
		// exists to end.
		slog.Error("convergence exhausted with NO owner notifier wired — the owner will not hear about this",
			"agent", rep.Name, "reason", rep.Reason)
	}

	// ── Path 2: best-effort, may fail silently ───────────────────────
	// Deliberately after the notice and deliberately unchecked: the owner
	// has already been told, so this is allowed to fail, to spawn nothing,
	// or to produce nothing.
	go s.spawnRecoveryAgent(rep)
}

// spawnRecoveryAgent creates a disposable per-incident diagnostician.
//
// Per-incident and short-lived on purpose. A standing recovery agent
// accumulates context and becomes the thing it was created to avoid, and
// the overseer is worse still — it is the busiest, largest-context agent
// in the fleet and therefore the most likely to be stuck itself. On
// 2026-08-10 it went down from a phantom session, from context-ceiling
// thrash, from daemon restarts, and from quota exhaustion: four unrelated
// causes in one day.
//
// Its brief is to diagnose and report, not to repair. It may hand work
// onward, but is required to succeed at nothing.
func (s *Server) spawnRecoveryAgent(rep IdleNudgeReport) {
	if s == nil || s.registry == nil {
		return
	}
	name := RecoveryAgentName(rep.Name)
	brief := FormatRecoveryBrief(rep)

	def, _, _, err := s.stitchAgentStart(name, s.workerWD, "", "", "ops_classify", s.overseerName(), "aside", "")
	if err != nil {
		slog.Warn("recovery agent not created; the owner notice already went out",
			"stuck", rep.Name, "err", err)
		return
	}
	if _, err := s.registry.Launch(def.Name); err != nil {
		slog.Warn("recovery agent not launched; the owner notice already went out",
			"stuck", rep.Name, "recovery", def.Name, "err", err)
		return
	}
	if _, err := s.sendToAgent(def.Name, brief, false); err != nil {
		slog.Warn("recovery agent not briefed; the owner notice already went out",
			"stuck", rep.Name, "recovery", def.Name, "err", err)
		return
	}
	slog.Info("recovery agent dispatched", "stuck", rep.Name, "recovery", def.Name)
}

// RecoveryAgentName is the disposable diagnostician's name for one
// incident. Derived from the stuck agent so a second exhaustion of the
// same agent reuses the seat rather than accumulating corpses.
func RecoveryAgentName(stuck string) string { return "recover-" + stuck }

// FormatRecoveryBrief is the diagnostician's one-shot instruction. Pure,
// so the wording — which carries the load-bearing "do not repair it"
// instruction — is testable without spawning anything.
func FormatRecoveryBrief(rep IdleNudgeReport) string {
	return fmt.Sprintf(
		"You are a disposable recovery agent. One job: work out WHY %q stopped making progress, and report it.\n\n"+
			"What is known: convergence gave up after %d nudges. Last verdict: %s. Last error: %s.\n\n"+
			"DO NOT restart, kill, or otherwise unstick %[1]q. It has been left in its failed state deliberately"+
			" so the cause is still visible; repairing it first destroys the evidence.\n\n"+
			"Look at its session, its transcript, the daemon log, and its mission. Decide whether the cause is the"+
			" agent, its session, its mission, or the machinery around it. File a bullseye target for what you find"+
			" (name + acceptance), then report back in one paragraph. If you cannot determine a cause, say so"+
			" plainly — an honest 'unknown' is worth more than a plausible guess.",
		rep.Name, DefaultIdleNudgeMax, orNone(rep.Reason), orNone(rep.Error))
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

// SetOwnerNotifier wires the deterministic owner-notice path (🎯T415).
func (s *Server) SetOwnerNotifier(n OwnerNotifier) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ownerNotifier = n
}

// ownerNotifierFunc adapts a plain function to OwnerNotifier, so main can
// wire server.NotifyOwnerNote without either package importing the other's
// types.
type ownerNotifierFunc func(subject, kind, text string) bool

func (f ownerNotifierFunc) NotifyOwner(subject, kind, text string) bool {
	return f(subject, kind, text)
}

// NewOwnerNotifier wraps a notify function as an OwnerNotifier.
func NewOwnerNotifier(f func(subject, kind, text string) bool) OwnerNotifier {
	return ownerNotifierFunc(f)
}
