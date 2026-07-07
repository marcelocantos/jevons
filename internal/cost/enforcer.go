// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"context"
	"fmt"
	"log/slog"
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
// only. Now is injected for deterministic tests.
type EnforcerArgs struct {
	Snapshot func() (*Snapshot, error)
	Config   func() *BudgetConfig
	Actions  Actions
	Notify   func(level Level, msg string)
	Now      func() time.Time
}

// Enforcer turns monitor alerts into clamp-down actions with
// warn → throttle → pause → kill escalation, enforces the spawn-halting
// hard ceiling, runs the dead-man's switch, and gates auto-resumes.
type Enforcer struct {
	snapshot func() (*Snapshot, error)
	config   func() *BudgetConfig
	actions  Actions
	notify   func(Level, string)
	now      func() time.Time

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
	lastHeartbeat  time.Time
	deadManFired   bool
	spawnHalted    bool
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
		now:            args.Now,
		lastLevel:      map[string]Level{},
		resumeAt:       map[string]time.Time{},
		resumeAttempts: map[string]int{},
	}
	if e.now == nil {
		e.now = time.Now
	}
	if e.notify == nil {
		e.notify = func(Level, string) {}
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

	// Reduce alerts to the max level per target.
	workerLevel := map[string]Level{}
	var fleetLevel, globalLevel Level
	hardCeiling := false
	infoSeen := map[string]bool{}
	for _, a := range snap.Alerts {
		switch a.Kind {
		case AlertWorkerRate:
			if a.Level > workerLevel[a.Worker] {
				workerLevel[a.Worker] = a.Level
			}
		case AlertFleetRate:
			if a.Level > fleetLevel {
				fleetLevel = a.Level
			}
		case AlertGlobalRate:
			if a.Level > globalLevel {
				globalLevel = a.Level
			}
		case AlertHardCeiling:
			hardCeiling = true
		case AlertSessionCount, AlertOrphanSessions, AlertProjection, AlertCollectorStale:
			infoSeen["!"+a.Kind] = true
			e.notifyOnce("!"+a.Kind, LevelWarn, a.Detail)
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

	// Per-worker escalation. Actions fire only when the level rises
	// above what was already acted on; when the alert clears, the slate
	// resets so a later breach re-escalates from scratch.
	for w, lvl := range workerLevel {
		prev := e.lastLevel[w]
		if lvl <= prev {
			continue
		}
		e.lastLevel[w] = lvl
		switch lvl {
		case LevelWarn:
			e.notify(lvl, fmt.Sprintf("budget: worker %s over warn threshold", w))
		case LevelThrottle:
			e.resumeAt[w] = e.now().Add(ThrottleCooldown)
			e.act(lvl, fmt.Sprintf("budget: throttling worker %s (paused %s)", w, ThrottleCooldown),
				func() error { return e.actions.PauseWorker(w) })
		case LevelPause:
			e.act(lvl, fmt.Sprintf("budget: pausing worker %s until spend clears", w),
				func() error { return e.actions.PauseWorker(w) })
		case LevelKill:
			if protected[w] {
				// Never deregister the butler's own brain — kill
				// downgrades to pause for protected workers.
				e.act(lvl, fmt.Sprintf("budget: worker %s over KILL threshold — protected, pausing instead", w),
					func() error { return e.actions.PauseWorker(w) })
			} else {
				e.act(lvl, fmt.Sprintf("budget: KILLING worker %s", w),
					func() error { return e.actions.KillWorker(w) })
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

	// Fleet and global escalation. Throttle/pause at fleet scope stop
	// the fleet resumably; kill reaches for the switch that also gets
	// detached processes. At global scope, maximum force is reserved
	// for the unattended case: with a recent owner heartbeat, kill
	// stops the fleet and screams instead of auto-firing the switch —
	// the burn may be the owner's own interactive session, and they
	// are present to decide.
	e.escalateScope("@fleet", fleetLevel,
		func() error { return e.actions.StopFleet() },
		func() error { return e.actions.KillSwitch() })
	attended := e.now().Sub(e.lastHeartbeat) < attendedGrace
	e.escalateScope("@global", globalLevel,
		func() error { return e.actions.StopFleet() },
		func() error {
			e.spawnHalted = true
			if attended {
				e.notify(LevelKill, "budget: global burn over KILL threshold — owner present, so fleet stopped resumably instead of firing the kill-switch; fire it manually if the burn is not yours")
				return e.actions.StopFleet()
			}
			return e.actions.KillSwitch()
		})

	// Hard ceiling: spawning halts; clears when the alert clears (spend
	// window rolls past, or the owner raises the ceiling).
	if hardCeiling && !e.spawnHalted {
		e.spawnHalted = true
		e.notify(LevelKill, "budget: hard daily ceiling crossed — spawning halted")
	} else if !hardCeiling && e.spawnHalted && globalLevel < LevelKill {
		e.spawnHalted = false
		e.notify(LevelWarn, "budget: clamp cleared — spawning re-enabled")
	}

	// Dead-man's switch: a burning fleet with no owner contact for the
	// configured idle stops resumably rather than running forever.
	if cfg.DeadManIdle > 0 && !e.deadManFired &&
		e.now().Sub(e.lastHeartbeat) > cfg.DeadManIdle.Std() &&
		(snap.FleetUSDPerHour > 0 || snap.GlobalUSDPerHour > 0) {
		e.deadManFired = true
		e.act(LevelPause, fmt.Sprintf("dead-man's switch: no owner contact for %s while burning — stopping fleet resumably", cfg.DeadManIdle.Std()),
			func() error { return e.actions.StopFleet() })
	}
}

// escalateScope applies fleet/global-scope escalation with the same
// only-upward semantics as workers.
func (e *Enforcer) escalateScope(scope string, lvl Level, pause, kill func() error) {
	prev := e.lastLevel[scope]
	if lvl == LevelNone {
		if prev != LevelNone {
			e.lastLevel[scope] = LevelNone
		}
		return
	}
	if lvl <= prev {
		return
	}
	e.lastLevel[scope] = lvl
	switch lvl {
	case LevelWarn:
		e.notify(lvl, fmt.Sprintf("budget: %s burn over warn threshold", scope[1:]))
	case LevelThrottle, LevelPause:
		e.act(lvl, fmt.Sprintf("budget: %s burn over %s threshold — stopping fleet resumably", scope[1:], lvl), pause)
	case LevelKill:
		e.act(lvl, fmt.Sprintf("budget: %s burn over KILL threshold — firing kill-switch", scope[1:]), kill)
	}
}

// act runs one enforcement action, notifying either way — a failed
// clamp-down must be loud, never silent.
func (e *Enforcer) act(lvl Level, msg string, f func() error) {
	if err := f(); err != nil {
		msg = fmt.Sprintf("%s — ACTION FAILED: %v", msg, err)
		slog.Error("cost enforcer", "msg", msg)
	} else {
		slog.Warn("cost enforcer", "msg", msg)
	}
	e.notify(lvl, msg)
}

// notifyOnce rate-limits informational signals to level transitions so
// a persisting warn doesn't spam every tick.
func (e *Enforcer) notifyOnce(kind string, lvl Level, detail string) {
	if e.lastLevel[kind] == lvl {
		return
	}
	e.lastLevel[kind] = lvl
	e.notify(lvl, "budget: "+detail)
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
