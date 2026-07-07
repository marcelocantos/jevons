// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"strings"
	"testing"
	"time"
)

// fakeActions records every enforcement action.
type fakeActions struct {
	paused     []string
	killed     []string
	fleetStops int
	killSwitch int
}

func (f *fakeActions) PauseWorker(id string) error { f.paused = append(f.paused, id); return nil }
func (f *fakeActions) KillWorker(id string) error  { f.killed = append(f.killed, id); return nil }
func (f *fakeActions) StopFleet() error            { f.fleetStops++; return nil }
func (f *fakeActions) KillSwitch() error           { f.killSwitch++; return nil }

// enforcerHarness builds an enforcer over canned snapshots and a fake
// action sink, with a controllable clock.
type enforcerHarness struct {
	e       *Enforcer
	acts    *fakeActions
	now     time.Time
	notices []string
}

func newHarness(cfg *BudgetConfig) *enforcerHarness {
	h := &enforcerHarness{
		acts: &fakeActions{},
		now:  time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC),
	}
	h.e = NewEnforcer(&EnforcerArgs{
		Snapshot: func() (*Snapshot, error) { return &Snapshot{}, nil },
		Config:   func() *BudgetConfig { return cfg },
		Actions:  h.acts,
		Notify:   func(_ Level, msg string) { h.notices = append(h.notices, msg) },
		Now:      func() time.Time { return h.now },
	})
	return h
}

func alertsOf(kind string, lvl Level, worker string) *Snapshot {
	return &Snapshot{Alerts: []Alert{{Kind: kind, Level: lvl, Worker: worker}}}
}

func TestEscalationLadder(t *testing.T) {
	h := newHarness(DefaultBudgetConfig())

	// Warn: notify only, no action.
	h.e.Act(alertsOf(AlertWorkerRate, LevelWarn, "po"))
	if len(h.acts.paused)+len(h.acts.killed) != 0 {
		t.Fatalf("warn acted: %+v", h.acts)
	}
	// Throttle: pause + cooldown gate on auto-resume.
	h.e.Act(alertsOf(AlertWorkerRate, LevelThrottle, "po"))
	if len(h.acts.paused) != 1 || h.acts.paused[0] != "po" {
		t.Fatalf("throttle did not pause: %+v", h.acts)
	}
	if err := h.e.AllowResume("po", true); err == nil {
		t.Fatal("throttled worker auto-resumed inside cooldown")
	}
	// A persisting alert at the same level does not re-fire the action.
	h.e.Act(alertsOf(AlertWorkerRate, LevelThrottle, "po"))
	if len(h.acts.paused) != 1 {
		t.Fatalf("same-level alert re-fired: %+v", h.acts.paused)
	}
	// Kill escalates — but only after confirmation (default 2 ticks).
	// The first kill tick pauses (reversible); the second kills.
	h.e.Act(alertsOf(AlertWorkerRate, LevelKill, "po"))
	if len(h.acts.killed) != 0 {
		t.Fatalf("kill fired before confirmation: %+v", h.acts)
	}
	h.e.Act(alertsOf(AlertWorkerRate, LevelKill, "po"))
	if len(h.acts.killed) != 1 || h.acts.killed[0] != "po" {
		t.Fatalf("confirmed kill did not kill: %+v", h.acts)
	}
	// Alert clears → slate resets → a fresh breach re-escalates.
	h.e.Act(&Snapshot{})
	pausedBefore := len(h.acts.paused)
	h.e.Act(alertsOf(AlertWorkerRate, LevelThrottle, "po"))
	if len(h.acts.paused) != pausedBefore+1 {
		t.Fatalf("post-clear breach did not re-act: %+v", h.acts.paused)
	}
}

// TestKillRequiresSustainedBreach is the flaw-fix oracle: a single-window
// kill-rate spike (e.g. the ~$11 idle-context re-cache tax reading as
// ~$132/hr for one window) must NOT nuke — it pauses reversibly and, if
// it decays, never kills. Only a sustained breach confirms.
func TestKillRequiresSustainedBreach(t *testing.T) {
	// One-off spike then decay: pause, no kill, streak resets.
	h := newHarness(DefaultBudgetConfig())
	h.e.Act(alertsOf(AlertWorkerRate, LevelKill, "po"))
	if len(h.acts.killed) != 0 || len(h.acts.paused) != 1 {
		t.Fatalf("spike: want pause-not-kill, got %+v", h.acts)
	}
	h.e.Act(&Snapshot{}) // decayed
	h.e.Act(alertsOf(AlertWorkerRate, LevelKill, "po"))
	if len(h.acts.killed) != 0 {
		t.Fatalf("a fresh spike after decay killed on tick 1 — streak did not reset: %+v", h.acts)
	}

	// Sustained: two consecutive kill ticks → kill.
	h2 := newHarness(DefaultBudgetConfig())
	h2.e.Act(alertsOf(AlertWorkerRate, LevelKill, "po"))
	h2.e.Act(alertsOf(AlertWorkerRate, LevelKill, "po"))
	if len(h2.acts.killed) != 1 {
		t.Fatalf("sustained breach did not confirm-kill: %+v", h2.acts)
	}

	// KillConfirmTicks=1 restores immediate kill for callers who want it.
	cfg := DefaultBudgetConfig()
	cfg.KillConfirmTicks = 1
	h3 := newHarness(cfg)
	h3.e.Act(alertsOf(AlertWorkerRate, LevelKill, "po"))
	if len(h3.acts.killed) != 1 {
		t.Fatalf("confirm=1 did not kill immediately: %+v", h3.acts)
	}
}

func TestProtectedWorkerNeverKilled(t *testing.T) {
	cfg := DefaultBudgetConfig() // protects "jevons"
	h := newHarness(cfg)
	h.e.Act(alertsOf(AlertWorkerRate, LevelKill, "jevons"))
	if len(h.acts.killed) != 0 {
		t.Fatalf("protected worker was killed: %+v", h.acts.killed)
	}
	if len(h.acts.paused) != 1 || h.acts.paused[0] != "jevons" {
		t.Fatalf("protected kill did not downgrade to pause: %+v", h.acts)
	}
}

func TestGlobalKillAttendedVsUnattended(t *testing.T) {
	// Attended: owner heard from recently → fleet stop + scream, no switch.
	h := newHarness(DefaultBudgetConfig())
	h.e.Heartbeat()
	h.e.Act(alertsOf(AlertGlobalRate, LevelKill, ""))
	if h.acts.killSwitch != 0 {
		t.Fatal("kill-switch fired while owner attended")
	}
	if h.acts.fleetStops != 1 {
		t.Fatalf("attended global kill did not stop fleet: %+v", h.acts)
	}
	if err := h.e.AllowSpawn(); err == nil {
		t.Fatal("global kill did not halt spawning")
	}

	// Unattended: silence beyond the grace → the switch fires, once the
	// breach is confirmed sustained.
	h2 := newHarness(DefaultBudgetConfig())
	h2.now = h2.now.Add(2 * time.Hour) // heartbeat was at construction
	h2.e.Act(alertsOf(AlertGlobalRate, LevelKill, ""))
	if h2.acts.killSwitch != 0 {
		t.Fatalf("kill-switch fired before confirmation: %+v", h2.acts)
	}
	h2.e.Act(alertsOf(AlertGlobalRate, LevelKill, ""))
	if h2.acts.killSwitch != 1 {
		t.Fatalf("confirmed unattended global kill did not fire the switch: %+v", h2.acts)
	}
}

func TestFleetKillFiresSwitch(t *testing.T) {
	h := newHarness(DefaultBudgetConfig())
	h.e.Act(alertsOf(AlertFleetRate, LevelKill, "")) // tick 1: pause pending
	h.e.Act(alertsOf(AlertFleetRate, LevelKill, "")) // tick 2: confirmed
	if h.acts.killSwitch != 1 {
		t.Fatalf("confirmed fleet kill did not fire the switch: %+v", h.acts)
	}
	if h.acts.fleetStops < 1 {
		t.Fatal("pending fleet kill did not stop the fleet reversibly first")
	}
}

func TestHardCeilingHaltsAndClears(t *testing.T) {
	h := newHarness(DefaultBudgetConfig())
	h.e.Act(alertsOf(AlertHardCeiling, LevelKill, ""))
	if err := h.e.AllowSpawn(); err == nil {
		t.Fatal("hard ceiling did not halt spawning")
	}
	if err := h.e.AllowResume("doc", false); err == nil {
		t.Fatal("hard ceiling did not block resume")
	}
	// Ceiling clears (window rolled / owner raised it) → spawning back.
	h.e.Act(&Snapshot{})
	if err := h.e.AllowSpawn(); err != nil {
		t.Fatalf("cleared ceiling still halted: %v", err)
	}
}

func TestDeadMansSwitch(t *testing.T) {
	cfg := DefaultBudgetConfig() // DeadManIdle 6h
	h := newHarness(cfg)

	// Burning but attended: no dead-man.
	h.e.Act(&Snapshot{FleetUSDPerHour: 1})
	if h.acts.fleetStops != 0 {
		t.Fatal("dead-man fired while attended")
	}
	// 7h of silence while burning → fleet stops, once.
	h.now = h.now.Add(7 * time.Hour)
	h.e.Act(&Snapshot{FleetUSDPerHour: 1})
	h.e.Act(&Snapshot{FleetUSDPerHour: 1})
	if h.acts.fleetStops != 1 {
		t.Fatalf("dead-man fired %d times, want exactly 1", h.acts.fleetStops)
	}
	// Owner returns → re-armed; more silence → fires again.
	h.e.Heartbeat()
	h.now = h.now.Add(8 * time.Hour)
	h.e.Act(&Snapshot{FleetUSDPerHour: 1})
	if h.acts.fleetStops != 2 {
		t.Fatalf("dead-man did not re-arm after heartbeat: %d", h.acts.fleetStops)
	}
	// Silence but NOT burning: nothing to stop, no fire.
	h.e.Heartbeat()
	h.now = h.now.Add(9 * time.Hour)
	h.e.Act(&Snapshot{})
	if h.acts.fleetStops != 2 {
		t.Fatal("dead-man fired on an idle fleet")
	}
}

func TestAutoResumeGuard(t *testing.T) {
	cfg := DefaultBudgetConfig() // cap 3
	h := newHarness(cfg)

	// Unattended resumes are capped.
	for i := 0; i < 3; i++ {
		if err := h.e.AllowResume("po", true); err != nil {
			t.Fatalf("auto-resume %d refused early: %v", i+1, err)
		}
	}
	err := h.e.AllowResume("po", true)
	if err == nil {
		t.Fatal("4th unattended resume allowed — the amplifier guard is off")
	}
	if !strings.Contains(err.Error(), "cap") {
		t.Fatalf("cap error not actionable: %v", err)
	}
	// Owner-initiated resume is not attempt-capped.
	if err := h.e.AllowResume("po", false); err != nil {
		t.Fatalf("owner resume refused: %v", err)
	}
	// Owner contact resets the counters.
	h.e.Heartbeat()
	if err := h.e.AllowResume("po", true); err != nil {
		t.Fatalf("heartbeat did not reset attempts: %v", err)
	}
	// A pause-clamped worker cannot resume until its level clears.
	h.e.Act(alertsOf(AlertWorkerRate, LevelPause, "doc"))
	if err := h.e.AllowResume("doc", false); err == nil {
		t.Fatal("pause-clamped worker resumed")
	}
	h.e.Act(&Snapshot{}) // clamp clears
	if err := h.e.AllowResume("doc", false); err != nil {
		t.Fatalf("cleared worker still blocked: %v", err)
	}
}

func TestThrottleCooldownExpires(t *testing.T) {
	h := newHarness(DefaultBudgetConfig())
	h.e.Act(alertsOf(AlertWorkerRate, LevelThrottle, "po"))
	if err := h.e.AllowResume("po", true); err == nil {
		t.Fatal("resume allowed inside throttle cooldown")
	}
	h.now = h.now.Add(ThrottleCooldown + time.Minute)
	h.e.Act(&Snapshot{}) // alert cleared; cooldown elapsed
	if err := h.e.AllowResume("po", true); err != nil {
		t.Fatalf("resume still blocked after cooldown: %v", err)
	}
}
