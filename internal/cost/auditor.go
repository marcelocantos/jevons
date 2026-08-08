// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"fmt"
	"strings"
	"time"
)

// Standing cost-safety auditor policy (🎯T334) on the T36 spine.
//
// Sensors (collector → monitor) already observe rates, session count,
// projected spend, and orphans. This layer names trip classes, resolves
// clamp posture (including overseer protection), and shapes durable
// audit + overseer-visible alert text. It is not a second ledger: USD
// truth stays in usage.db + monitor snapshots.

// EventSource is stamped on overseer-directed cost-safety pushes.
const EventSource = "cost-safety"

// Eventlog component for clamp decision audit trail.
const AuditComponent = "cost_clamp"

// Named trip classes — hermetic policy oracles key off these strings.
const (
	TripNone             = "none"
	TripGlobalRate       = "global-rate"
	TripFleetRate        = "fleet-rate"
	TripWorkerRate       = "worker-rate"
	TripSessionCount     = "session-count"
	TripSpawnStorm       = "spawn-storm"
	TripOrphanSessions   = "orphan-sessions"
	TripProjectedSpend   = "projected-overspend"
	TripHardCeiling      = "hard-ceiling"
	TripCollectorStale   = "collector-stale"
	TripDeadMan          = "dead-man"
	TripSpawnHalt        = "spawn-halt"
	TripSpawnResume      = "spawn-resume"
	TripInformational    = "informational"
	TripProtectedOverseer = "protected-overseer"
)

// Clamp action labels for audit trail and policy tests.
const (
	ActionNone            = "none"
	ActionWarn            = "warn"
	ActionThrottle        = "throttle"
	ActionPause           = "pause"
	ActionKill            = "kill"
	ActionSpawnHalt       = "spawn_halt"
	ActionInformational   = "informational"
	ActionProtectedPause  = "protected_pause" // kill/pause of overseer → never kill
	ActionProtectedSkip   = "protected_skip"  // no process kill of protected worker
)

// AlertSpawnStorm is the concurrent-session storm signal (postmortem: 47
// sessions). Monitor emits it when session count is at least
// SpawnStormFactor × MaxSessions (and MaxSessions > 0).
const AlertSpawnStorm = "spawn-storm"

// SpawnStormFactor: session count ≥ MaxSessions * factor trips spawn-storm
// (in addition to the ordinary session-count warn at MaxSessions).
const SpawnStormFactor = 2

// Trip is one named cost-safety trip class resolved from a sensor alert.
type Trip struct {
	// Class is the stable trip-class id (TripFleetRate, …).
	Class string `json:"class"`
	// Kind is the underlying monitor alert kind (may equal Class).
	Kind string `json:"kind"`
	// Level is the sensor escalation level.
	Level Level `json:"level"`
	// Worker is set for per-worker trips.
	Worker string `json:"worker,omitempty"`
	// Detail is the sensor detail line.
	Detail string `json:"detail,omitempty"`
	// Action is the clamp posture after protection rules.
	Action string `json:"action"`
	// OverseerProtected is true when the target is budget-protected
	// (overseer never killed — T137/T139 class).
	OverseerProtected bool `json:"overseer_protected,omitempty"`
	// Informational means global/foreign burn: notify only, never clamp fleet.
	Informational bool `json:"informational,omitempty"`
}

// ClampDecision is one durable audit-trail row for a clamp-down act or
// notify. Wired into eventlog as component=cost_clamp.
type ClampDecision struct {
	At        time.Time `json:"at"`
	TripClass string    `json:"trip_class"`
	Level     Level     `json:"level"`
	// Decision is the enforcer outcome kind: action | notify | spawn_halt |
	// spawn_resume | protected | informational | dead_man.
	Decision string `json:"decision"`
	// Action is the clamp posture (ActionKill, ActionPause, …).
	Action string `json:"action"`
	// Target is worker id, @fleet, @global, or empty.
	Target  string `json:"target,omitempty"`
	Message string `json:"message"`
	// Outcome: ok | error | skipped_protected.
	Outcome string `json:"outcome,omitempty"`
}

// TripClassForKind maps a monitor alert kind to a trip class id.
func TripClassForKind(kind string) string {
	switch kind {
	case AlertGlobalRate:
		return TripGlobalRate
	case AlertFleetRate:
		return TripFleetRate
	case AlertWorkerRate:
		return TripWorkerRate
	case AlertSessionCount:
		return TripSessionCount
	case AlertSpawnStorm:
		return TripSpawnStorm
	case AlertOrphanSessions:
		return TripOrphanSessions
	case AlertProjection:
		return TripProjectedSpend
	case AlertHardCeiling:
		return TripHardCeiling
	case AlertCollectorStale:
		return TripCollectorStale
	default:
		if kind == "" {
			return TripNone
		}
		return kind
	}
}

// ResolveTrip classifies one monitor alert into a trip with clamp posture.
// protected lists workers that must never be budget-killed (overseer).
func ResolveTrip(a Alert, protected map[string]bool) Trip {
	class := TripClassForKind(a.Kind)
	t := Trip{
		Class:  class,
		Kind:   a.Kind,
		Level:  a.Level,
		Worker: a.Worker,
		Detail: a.Detail,
	}

	// Global burn is always informational — jevons does not kill owner sessions.
	if a.Kind == AlertGlobalRate {
		t.Informational = true
		t.Action = ActionInformational
		t.Class = TripGlobalRate
		return t
	}

	// Soft / hygiene signals: warn path only (enforcer notifyOnce).
	switch a.Kind {
	case AlertSessionCount, AlertOrphanSessions, AlertProjection, AlertCollectorStale:
		t.Action = ActionWarn
		if a.Level == LevelNone {
			t.Action = ActionNone
		}
		return t
	case AlertSpawnStorm:
		// Storm is a harder session-count signal; clamp posture follows level
		// (monitor may emit throttle+). Fleet enforcer still owns fleet stop.
		t.Action = actionForLevel(a.Level)
		return t
	}

	// USD / hard ceiling / worker-fleet rates.
	if a.Worker != "" && protected[a.Worker] {
		t.OverseerProtected = true
		if a.Level >= LevelKill {
			t.Action = ActionProtectedPause
			t.Class = TripProtectedOverseer
		} else if a.Level >= LevelThrottle {
			// Protected workers are not paused by fleetActions either; policy
			// records the intent as protected_skip for audit honesty.
			t.Action = ActionProtectedSkip
			t.Class = TripProtectedOverseer
		} else {
			t.Action = ActionWarn
		}
		return t
	}

	t.Action = actionForLevel(a.Level)
	if a.Kind == AlertHardCeiling && a.Level >= LevelKill {
		// Hard ceiling's product action is spawn-halt (+ optional fleet).
		if t.Action == ActionKill {
			t.Action = ActionSpawnHalt
		}
	}
	return t
}

func actionForLevel(lvl Level) string {
	switch lvl {
	case LevelWarn:
		return ActionWarn
	case LevelThrottle:
		return ActionThrottle
	case LevelPause:
		return ActionPause
	case LevelKill:
		return ActionKill
	default:
		return ActionNone
	}
}

// ClassifySnapshot resolves every alert on a snapshot into trips.
func ClassifySnapshot(snap *Snapshot, protected []string) []Trip {
	if snap == nil {
		return nil
	}
	prot := map[string]bool{}
	for _, w := range protected {
		if w != "" {
			prot[w] = true
		}
	}
	out := make([]Trip, 0, len(snap.Alerts))
	for _, a := range snap.Alerts {
		if a.Level == LevelNone && a.Kind == "" {
			continue
		}
		out = append(out, ResolveTrip(a, prot))
	}
	return out
}

// OverseerNeverKilled reports whether any trip would kill a protected worker.
// Oracle for "overseer process never killed by budget".
func OverseerNeverKilled(trips []Trip) bool {
	for _, t := range trips {
		if t.OverseerProtected && t.Action == ActionKill {
			return false
		}
		if t.Worker != "" && t.OverseerProtected && t.Action == ActionKill {
			return false
		}
	}
	return true
}

// AuditFields builds eventlog / slog fields for a clamp decision (🎯T334).
func AuditFields(d ClampDecision) map[string]any {
	fields := map[string]any{
		"trip_class": d.TripClass,
		"level":      d.Level.String(),
		"action":     d.Action,
		"msg":        d.Message,
	}
	if d.Target != "" {
		fields["target"] = d.Target
	}
	if d.Outcome != "" {
		fields["outcome"] = d.Outcome
	}
	if !d.At.IsZero() {
		fields["at"] = d.At.UTC().Format(time.RFC3339)
	}
	return fields
}

// FormatOverseerAlert builds owner/overseer-visible clamp alert body
// (caller wraps with butler.FormatEventPush(EventSource, …)).
func FormatOverseerAlert(level Level, msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		msg = "cost-safety trip (no detail)"
	}
	return fmt.Sprintf("COST-SAFETY [%s]: %s", level.String(), msg)
}

// InferTripClassFromMessage best-effort maps an enforcer notify line to a
// trip class when structured trip context is unavailable (notify path).
func InferTripClassFromMessage(msg string) string {
	m := strings.ToLower(msg)
	switch {
	case strings.Contains(m, "dead-man"):
		return TripDeadMan
	case strings.Contains(m, "spawn-storm"), strings.Contains(m, "spawn storm"):
		return TripSpawnStorm
	case strings.Contains(m, "spawning re-enabled"), strings.Contains(m, "clamp cleared"):
		return TripSpawnResume
	case strings.Contains(m, "spawning halted"):
		return TripSpawnHalt
	case strings.Contains(m, "protected"):
		return TripProtectedOverseer
	case strings.Contains(m, "total machine burn"), strings.Contains(m, "informational"):
		return TripInformational
	case strings.Contains(m, "fleet burn"), strings.Contains(m, "fleet "):
		return TripFleetRate
	case strings.Contains(m, "worker "):
		return TripWorkerRate
	case strings.Contains(m, "hard ceiling"), strings.Contains(m, "hard daily"):
		return TripHardCeiling
	case strings.Contains(m, "orphan"):
		return TripOrphanSessions
	case strings.Contains(m, "projected"):
		return TripProjectedSpend
	case strings.Contains(m, "collector"):
		return TripCollectorStale
	case strings.Contains(m, "session"):
		return TripSessionCount
	default:
		return TripInformational
	}
}

// NewClampDecision builds a decision row for the audit trail.
func NewClampDecision(at time.Time, level Level, decision, action, target, message, outcome string) ClampDecision {
	if action == "" {
		action = actionForLevel(level)
	}
	return ClampDecision{
		At:        at,
		TripClass: InferTripClassFromMessage(message),
		Level:     level,
		Decision:  decision,
		Action:    action,
		Target:    target,
		Message:   message,
		Outcome:   outcome,
	}
}
