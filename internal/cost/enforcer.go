// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Actions is the mechanism seam through which the enforcer touches the
// world. Production wires these to the claudia fleet and the tmux
// kill-switch; tests and the drill supply their own.
type Actions interface {
	// PauseWorker stops a worker's process resumably (thread persists).
	PauseWorker(id string) error
	// KillWorker stops and deregisters a worker.
	KillWorker(id string) error
	// StopFleet stops every jevons-owned worker resumably (dead-man).
	StopFleet() error
	// KillSwitch is the global stop: it must reach launchd-detached
	// fleet processes, not just registry-tracked ones.
	KillSwitch() error
}

// EnforcerArgs parameterises NewEnforcer. Snapshot, Config, and Actions
// are required; production passes Monitor.Snapshot. Notify surfaces
// enforcement actions to the owner (chat broadcast + log); nil = log
// only. AuditTrail records structured clamp decisions for the durable
// eventlog (🎯T334 standing cost-safety); nil skips structured audit.
// Now is injected for deterministic tests.
type EnforcerArgs struct {
	Snapshot   func() (*Snapshot, error)
	Config     func() *BudgetConfig
	Actions    Actions
	Notify     func(level Level, msg string)
	AuditTrail func(ClampDecision)
	Now        func() time.Time
}

// Enforcer turns monitor alerts into clamp-down actions with
// warn → throttle → pause → kill escalation, enforces the spawn-halting
// hard ceiling, runs the dead-man's switch, and gates auto-resumes.
// Standing cost-safety (🎯T334) dual-writes clamp decisions via AuditTrail.
type Enforcer struct {
	snapshot   func() (*Snapshot, error)
	config     func() *BudgetConfig
	actions    Actions
	notify     func(Level, string)
	auditTrail func(ClampDecision)
	now        func() time.Time

	mu sync.Mutex
	// lastLevel tracks the acted-on level per target ("" = fleet/global)
	// so a persisting alert doesn't re-fire the same action every tick,
	// while a higher level still escalates.
	lastLevel map[string]Level
	// resumeAt blocks a throttled worker's auto-resume until the
	// cooldown passes (the duty-cycle throttle).
	resumeAt map[string]time.Time
	// resumeAttempts counts unattended relaunches per worker since the
	// last owner contact (the auto-resume amplifier guard).
	resumeAttempts map[string]int
	// killStreak counts consecutive kill-level ticks per target, so an
	// irreversible action fires only on a sustained breach, not a spike.
	killStreak     map[string]int
	lastHeartbeat  time.Time
	deadManFired   bool
	spawnHalted    bool
	lastGlobalInfo Level // last-notified informational global level
	lastSnap       *Snapshot
}

// ThrottleCooldown is how long a throttled worker stays paused before
// auto-resume is allowed again (the off-phase of the duty cycle).
const ThrottleCooldown = 10 * time.Minute

// attendedGrace: maximum force is for the UNATTENDED case (the 2026-07-06
// incident). If the owner was heard from within this window, a global
// kill-level breach stops the fleet resumably and screams, leaving the
// kill-switch decision to the human who is demonstrably present — their
// own heavy interactive session may be the very burn that tripped it.
const attendedGrace = 30 * time.Minute

// NewEnforcer constructs an Enforcer. The owner heartbeat starts at
// construction time — the daemon booting is evidence of an owner.
func NewEnforcer(args *EnforcerArgs) *Enforcer {
	e := &Enforcer{
		snapshot:       args.Snapshot,
		config:         args.Config,
		actions:        args.Actions,
		notify:         args.Notify,
		auditTrail:     args.AuditTrail,
		now:            args.Now,
		lastLevel:      map[string]Level{},
		resumeAt:       map[string]time.Time{},
		resumeAttempts: map[string]int{},
		killStreak:     map[string]int{},
	}
	if e.now == nil {
		e.now = time.Now
	}
	if e.notify == nil {
		e.notify = func(Level, string) {}
	}
	if e.auditTrail == nil {
		e.auditTrail = func(ClampDecision) {}
	}
	e.lastHeartbeat = e.now()
	return e
}

// Heartbeat records owner contact: any chat message, UI connection, or
// owner-driven direct. It re-arms the dead-man's switch and resets the
// per-worker auto-resume attempt counters.
func (e *Enforcer) Heartbeat() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.lastHeartbeat = e.now()
	e.deadManFired = false
	e.resumeAttempts = map[string]int{}
}

// AllowSpawn reports whether launching a NEW worker is currently
// permitted. It fails when the hard daily ceiling has been crossed or a
// global kill is in force.
func (e *Enforcer) AllowSpawn() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.spawnHalted {
		return fmt.Errorf("spawning halted by budget clamp-down (hard ceiling or global kill) — raise the ceiling in budget.json or wait for the window to clear")
	}
	return nil
}

// AllowResume reports whether a worker's process may be (re)started.
// auto marks unattended relaunches (rehydrate-on-GC, daemon StartAll,
// crash recovery), which are budget-checked AND attempt-capped; owner-
// initiated resumes are budget-checked only — the owner asking is
// itself a heartbeat.
func (e *Enforcer) AllowResume(id string, auto bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.spawnHalted {
		return fmt.Errorf("resume %q blocked: budget clamp-down in force", id)
	}
	if until, ok := e.resumeAt[id]; ok && e.now().Before(until) {
		return fmt.Errorf("resume %q blocked: throttled until %s", id, until.Format(time.RFC3339))
	}
	if lvl := e.lastLevel[id]; lvl >= LevelPause {
		return fmt.Errorf("resume %q blocked: worker is %s-clamped; spend must drop below the %s threshold first", id, lvl, lvl)
	}
	if auto {
		cfg := e.config()
		if cfg.ResumeMaxAttempts > 0 && e.resumeAttempts[id] >= cfg.ResumeMaxAttempts {
			return fmt.Errorf("resume %q blocked: %d unattended resume attempts since last owner contact (cap %d)",
				id, e.resumeAttempts[id], cfg.ResumeMaxAttempts)
		}
		e.resumeAttempts[id]++
	}
	return nil
}

// Snapshot returns the most recent snapshot the enforcer acted on (nil
// before the first tick). The API layer serves this rather than
// recomputing.
func (e *Enforcer) LastSnapshot() *Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastSnap
}

// Tick computes a snapshot and enforces it. Returns the snapshot.
func (e *Enforcer) Tick() (*Snapshot, error) {
	snap, err := e.snapshot()
	if err != nil {
		return nil, err
	}
	e.Act(snap)
	return snap, nil
}

// Act applies the escalation policy to one snapshot. Exported so tests
// and the drill can drive it with controlled snapshots.
func (e *Enforcer) Act(snap *Snapshot) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.lastSnap = snap
	cfg := e.config()
	// 🎯T137: under subscription accounting, API-list-price USD must never
	// drive pause/kill/spawn-halt — defense in depth even if a monitor
	// alert slips through above LevelWarn.
	sub := cfg.IsSubscription()

	// Reduce alerts to the max level per target.
	workerLevel := map[string]Level{}
	var fleetLevel, globalLevel Level
	hardCeiling := false
	infoSeen := map[string]bool{}
	for _, a := range snap.Alerts {
		lvl := a.Level
		switch a.Kind {
		case AlertWorkerRate:
			if sub && lvl > LevelWarn {
				lvl = LevelWarn
			}
			if lvl > workerLevel[a.Worker] {
				workerLevel[a.Worker] = lvl
			}
		case AlertFleetRate:
			if sub && lvl > LevelWarn {
				lvl = LevelWarn
			}
			if lvl > fleetLevel {
				fleetLevel = lvl
			}
		case AlertGlobalRate:
			if sub && lvl > LevelWarn {
				lvl = LevelWarn
			}
			if lvl > globalLevel {
				globalLevel = lvl
			}
		case AlertHardCeiling:
			if sub {
				// Informational only — never halt spawns on estimate $.
				infoSeen["!"+a.Kind] = true
				e.notifyOnce("!"+a.Kind, LevelWarn, a.Detail)
			} else {
				hardCeiling = true
			}
		case AlertSessionCount, AlertOrphanSessions, AlertProjection, AlertCollectorStale, AlertSpawnStorm:
			// Spawn storm (🎯T334) is notify-level here; session hygiene does
			// not alone fire kill-switch — fleet/worker rate ladders do.
			infoSeen["!"+a.Kind] = true
			lvl := a.Level
			if lvl == LevelNone {
				lvl = LevelWarn
			}
			e.notifyOnce("!"+a.Kind, lvl, a.Detail)
		}
	}
	// Cleared informational signals re-arm so the next episode notifies.
	for k := range e.lastLevel {
		if len(k) > 0 && k[0] == '!' && !infoSeen[k] {
			e.lastLevel[k] = LevelNone
		}
	}

	protected := map[string]bool{}
	for _, w := range cfg.ProtectedWorkers {
		protected[w] = true
	}

	// Reset the kill-confirmation streak for any target no longer at
	// kill level (a spike that decayed).
	e.decayKillStreaks(workerLevel, fleetLevel, globalLevel)

	// Per-worker escalation. Actions fire only when the level rises
	// above what was already acted on; when the alert clears, the slate
	// resets so a later breach re-escalates from scratch. Kill is
	// special: it applies pause immediately (stopping the burn
	// reversibly) and only escalates to the irreversible kill once the
	// breach has persisted KillConfirmTicks ticks.
	for w, lvl := range workerLevel {
		prev := e.lastLevel[w]
		if lvl < LevelKill && lvl <= prev {
			continue
		}
		switch lvl {
		case LevelWarn:
			e.lastLevel[w] = lvl
			msg := fmt.Sprintf("budget: worker %s over warn threshold", w)
			e.notify(lvl, msg)
			e.recordAudit(lvl, "notify", ActionWarn, w, msg, "ok")
		case LevelThrottle:
			e.lastLevel[w] = lvl
			e.resumeAt[w] = e.now().Add(ThrottleCooldown)
			e.act(lvl, w, fmt.Sprintf("budget: throttling worker %s (paused %s)", w, ThrottleCooldown),
				func() error { return e.actions.PauseWorker(w) })
		case LevelPause:
			e.lastLevel[w] = lvl
			e.act(lvl, w, fmt.Sprintf("budget: pausing worker %s until spend clears", w),
				func() error { return e.actions.PauseWorker(w) })
		case LevelKill:
			key := "w:" + w
			if protected[w] {
				// Never deregister the butler's own brain — kill
				// downgrades to pause for protected workers.
				e.lastLevel[w] = LevelPause
				e.act(LevelKill, w, fmt.Sprintf("budget: worker %s over KILL threshold — protected, pausing instead", w),
					func() error { return e.actions.PauseWorker(w) })
			} else if e.killReady(key) {
				e.lastLevel[w] = LevelKill
				e.act(LevelKill, w, fmt.Sprintf("budget: KILLING worker %s (kill breach confirmed)", w),
					func() error { return e.actions.KillWorker(w) })
			} else {
				// Pending: stop the burn reversibly, keep re-evaluating.
				e.lastLevel[w] = LevelPause
				e.act(LevelPause, w, fmt.Sprintf("budget: worker %s at KILL threshold — pausing pending confirmation (%d/%d)", w, e.killStreak[key], e.confirmTicks()),
					func() error { return e.actions.PauseWorker(w) })
			}
		}
	}
	for w, prev := range e.lastLevel {
		if len(w) == 0 || w[0] == '@' || w[0] == '!' {
			continue // scope and signal entries have their own lifecycles
		}
		if _, still := workerLevel[w]; !still && prev != LevelNone {
			e.lastLevel[w] = LevelNone
		}
	}

	// Fleet escalation — this drives the destructive path, because the
	// "fleet" scope is what jevons OWNS (attributed workers + orphans
	// lost inside the fleet, which is the 2026-07-06 incident's form).
	// Throttle/pause stop the fleet resumably; kill reaches the switch
	// that also gets launchd-detached processes. Maximum force is for
	// the unattended case: with a recent owner heartbeat, a fleet-kill
	// breach stops the fleet and screams rather than auto-firing the
	// switch, leaving that call to the demonstrably-present owner.
	attended := e.now().Sub(e.lastHeartbeat) < attendedGrace
	e.escalateScope("@fleet", fleetLevel,
		func() error { return e.actions.StopFleet() },
		func() error {
			if attended {
				msg := "budget: fleet burn over KILL threshold — owner present, so fleet stopped resumably instead of firing the kill-switch; fire it manually if needed"
				e.notify(LevelKill, msg)
				e.recordAudit(LevelKill, "action", ActionPause, "@fleet", msg, "ok")
				return e.actions.StopFleet()
			}
			return e.actions.KillSwitch()
		})

	// Global burn is INFORMATIONAL: jevons can neither kill the owner's
	// own (foreign) sessions nor should it nuke its fleet over them. It
	// surfaces total-machine burn for awareness only, on level changes.
	if globalLevel != e.lastGlobalInfo {
		e.lastGlobalInfo = globalLevel
		if globalLevel != LevelNone {
			msg := fmt.Sprintf("total machine burn is %s-level (%.2f USD/hr) — informational; jevons clamps its own fleet, not your sessions", globalLevel, snap.GlobalUSDPerHour)
			e.notify(globalLevel, msg)
			e.recordAudit(globalLevel, "informational", ActionInformational, "@global", msg, "ok")
		}
	}

	// Spawn halt: a cheap, reversible guard that engages on a fleet-kill
	// breach or the hard daily ceiling — including the pending tick,
	// since stopping NEW spend costs nothing and reverses freely. Global
	// (foreign) burn does NOT halt jevons's own spawns.
	haltSpawning := hardCeiling || fleetLevel == LevelKill
	if haltSpawning && !e.spawnHalted {
		e.spawnHalted = true
		msg := "budget: spawning halted (global kill breach or hard daily ceiling)"
		e.notify(LevelKill, msg)
		e.recordAudit(LevelKill, "spawn_halt", ActionSpawnHalt, "", msg, "ok")
	} else if !haltSpawning && e.spawnHalted {
		e.spawnHalted = false
		msg := "budget: clamp cleared — spawning re-enabled"
		e.notify(LevelWarn, msg)
		e.recordAudit(LevelWarn, "spawn_resume", ActionNone, "", msg, "ok")
	}

	// Dead-man's switch: a burning fleet with no owner contact for the
	// configured idle stops resumably rather than running forever.
	if cfg.DeadManIdle > 0 && !e.deadManFired &&
		e.now().Sub(e.lastHeartbeat) > cfg.DeadManIdle.Std() &&
		(snap.FleetUSDPerHour > 0 || snap.GlobalUSDPerHour > 0) {
		e.deadManFired = true
		e.act(LevelPause, "@fleet", fmt.Sprintf("dead-man's switch: no owner contact for %s while burning — stopping fleet resumably", cfg.DeadManIdle.Std()),
			func() error { return e.actions.StopFleet() })
	}
}

// escalateScope applies fleet/global-scope escalation with the same
// only-upward semantics as workers. Kill applies a resumable fleet stop
// immediately and only fires the irreversible kill (the switch) once the
// breach has persisted KillConfirmTicks ticks.
func (e *Enforcer) escalateScope(scope string, lvl Level, pause, kill func() error) {
	prev := e.lastLevel[scope]
	if lvl == LevelNone {
		if prev != LevelNone {
			e.lastLevel[scope] = LevelNone
		}
		return
	}
	if lvl < LevelKill && lvl <= prev {
		return
	}
	switch lvl {
	case LevelWarn:
		e.lastLevel[scope] = lvl
		msg := fmt.Sprintf("budget: %s burn over warn threshold", scope[1:])
		e.notify(lvl, msg)
		e.recordAudit(lvl, "notify", ActionWarn, scope, msg, "ok")
	case LevelThrottle, LevelPause:
		e.lastLevel[scope] = lvl
		e.act(lvl, scope, fmt.Sprintf("budget: %s burn over %s threshold — stopping fleet resumably", scope[1:], lvl), pause)
	case LevelKill:
		if e.killReady(scope) {
			e.lastLevel[scope] = LevelKill
			e.act(LevelKill, scope, fmt.Sprintf("budget: %s burn over KILL threshold (confirmed) — firing kill-switch", scope[1:]), kill)
		} else {
			e.lastLevel[scope] = LevelPause
			e.act(LevelPause, scope, fmt.Sprintf("budget: %s burn over KILL threshold — stopping fleet resumably pending confirmation (%d/%d)", scope[1:], e.killStreak[scope], e.confirmTicks()), pause)
		}
	}
}

// confirmTicks is the number of consecutive kill-level ticks required
// before an irreversible action fires (minimum 1).
func (e *Enforcer) confirmTicks() int {
	if n := e.config().KillConfirmTicks; n > 1 {
		return n
	}
	return 1
}

// killReady increments target's kill-confirmation streak and reports
// whether the irreversible action may now fire.
func (e *Enforcer) killReady(target string) bool {
	e.killStreak[target]++
	return e.killStreak[target] >= e.confirmTicks()
}

// decayKillStreaks resets the confirmation streak for any target not
// currently at kill level, so a spike that decays doesn't leave a worker
// one tick from the nuke.
func (e *Enforcer) decayKillStreaks(workerLevel map[string]Level, fleetLevel, globalLevel Level) {
	atKill := map[string]bool{}
	for w, lvl := range workerLevel {
		if lvl == LevelKill {
			atKill["w:"+w] = true
		}
	}
	if fleetLevel == LevelKill {
		atKill["@fleet"] = true
	}
	if globalLevel == LevelKill {
		atKill["@global"] = true
	}
	for k := range e.killStreak {
		if !atKill[k] {
			delete(e.killStreak, k)
		}
	}
}

// act runs one enforcement action, notifying either way — a failed
// clamp-down must be loud, never silent. Dual-writes the audit trail (🎯T334).
func (e *Enforcer) act(lvl Level, target, msg string, f func() error) {
	outcome := "ok"
	if err := f(); err != nil {
		msg = fmt.Sprintf("%s — ACTION FAILED: %v", msg, err)
		slog.Error("cost enforcer", "msg", msg)
		outcome = "error"
	} else {
		slog.Warn("cost enforcer", "msg", msg)
	}
	e.notify(lvl, msg)
	e.recordAudit(lvl, "action", actionForLevel(lvl), target, msg, outcome)
}

// notifyOnce rate-limits informational signals to level transitions so
// a persisting warn doesn't spam every tick.
func (e *Enforcer) notifyOnce(kind string, lvl Level, detail string) {
	if e.lastLevel[kind] == lvl {
		return
	}
	e.lastLevel[kind] = lvl
	msg := "budget: " + detail
	e.notify(lvl, msg)
	e.recordAudit(lvl, "notify", actionForLevel(lvl), "", msg, "ok")
}

// recordAudit emits one structured clamp decision for the eventlog path.
func (e *Enforcer) recordAudit(lvl Level, decision, action, target, msg, outcome string) {
	if action == "" {
		action = actionForLevel(lvl)
	}
	// Protected-worker kill paths leave lastLevel at pause and msg says protected.
	if strings.Contains(strings.ToLower(msg), "protected") {
		action = ActionProtectedPause
		if outcome == "ok" {
			outcome = "skipped_protected"
		}
	}
	d := NewClampDecision(e.now(), lvl, decision, action, target, msg, outcome)
	e.auditTrail(d)
}

// Run drives the enforcement loop until ctx is cancelled.
func (e *Enforcer) Run(ctx context.Context, every time.Duration) {
	if every == 0 {
		every = 15 * time.Second
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := e.Tick(); err != nil {
				slog.Warn("cost enforcer: tick", "err", err)
			}
		}
	}
}
