// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/converge"
)

// recordingHuman is a hermetic HumanSink.
type recordingHuman struct {
	raised  []string
	cleared []string
}

func (r *recordingHuman) RaiseImpatienceAlert(agent, text string) error {
	r.raised = append(r.raised, agent+"|"+text)
	return nil
}

func (r *recordingHuman) ClearImpatienceAlert(agent string) error {
	r.cleared = append(r.cleared, agent)
	return nil
}

// recordingOverseer is a hermetic OverseerSink.
type recordingOverseer struct {
	events []string
}

func (r *recordingOverseer) PushConvergeEvent(class, text string) error {
	r.events = append(r.events, class+"|"+text)
	return nil
}

// recordingRepressure is a hermetic RePressureSink.
type recordingRepressure struct {
	agents []string
}

func (r *recordingRepressure) RePressure(agent, mission string) error {
	r.agents = append(r.agents, agent+"/"+mission)
	return nil
}

func impatienceFixture(t *testing.T, now time.Time) (*Server, *IdleActivityTracker) {
	t.Helper()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{
		{Name: "jevons", WorkDir: dir, SessionID: "s-o", Purpose: claudia.PurposeOverseer,
			Materialized: true, Provider: "grok", AutoStart: true},
		{Name: "jv-t317-stuck", WorkDir: dir, SessionID: "s-w", Purpose: claudia.PurposeWork,
			Parent: "jevons-po", Materialized: true, Provider: "grok", AutoStart: true, TargetID: "T317"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	ledger, err := OpenIdleNudgeLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	activity := NewIdleActivityTracker()
	// Gap dwell starts at first unsatisfied observe; phase stays idle.
	activity.by["jv-t317-stuck"] = IdleActivity{Phase: "idle", Updated: now}
	return &Server{registry: reg, idleActivity: activity, idleNudgeLedger: ledger}, activity
}

// TestImpatienceEngineEscalatesFromIdlePressureSweep pins the 🎯T317 daemon
// glue: idlePressureSweep with an installed engine opens a T316 gap, climbs
// the ladder, and actuates overseer noise without satisfying the gap.
func TestImpatienceEngineEscalatesFromIdlePressureSweep(t *testing.T) {
	t.Parallel()
	now := time.Unix(10_000, 0)
	s, activity := impatienceFixture(t, now)

	rep := &recordingRepressure{}
	ov := &recordingOverseer{}
	hum := &recordingHuman{}
	eng := NewImpatienceEngine(ImpatienceEngineArgs{
		Sinks: converge.Sinks{RePressure: rep, Overseer: ov, Human: hum},
	})
	s.SetImpatienceEngine(eng)
	s.SetIdlePressureHooks(IdlePressureHooks{
		MissionOpen: func(string) bool { return true },
	})

	running := func(name string) bool { return name == "jv-t317-stuck" }

	// First tick at t0: gap opens, no rung yet (grace).
	s.idlePressureSweep(idlePressureDeps{Now: now, Running: running})
	eng.mu.Lock()
	open := eng.set.Len()
	eng.mu.Unlock()
	if open != 1 {
		t.Fatalf("want 1 standing gap after idle observe, got %d", open)
	}
	if len(rep.agents) != 0 || len(ov.events) != 0 {
		t.Fatalf("grace period must not fire: rep=%v ov=%v", rep.agents, ov.events)
	}

	// Past re-pressure grace (4m). Delivered re-pressure is weak T318 progress:
	// it buys Credit (8m) of delay, so later rungs need raw dwell ≥ threshold+credit.
	const attenCredit = 8 * time.Minute
	tRep := now.Add(converge.RepressureAfter + time.Minute)
	s.idlePressureSweep(idlePressureDeps{Now: tRep, Running: running})
	if len(rep.agents) != 1 || !strings.HasPrefix(rep.agents[0], "jv-t317-stuck/") {
		t.Fatalf("re-pressure rung: got %v", rep.agents)
	}
	eng.mu.Lock()
	still := eng.set.Len()
	tracked := eng.ladder.Tracked("jv-t317-stuck")
	eng.mu.Unlock()
	if still != 1 || !tracked {
		t.Fatalf("noise must not satisfy: open=%d tracked=%v", still, tracked)
	}

	// Jump past overseer threshold + attenuation credit (no intermediate ticks,
	// so credit stays at one re-pressure signal).
	tOv := now.Add(converge.OverseerNoiseAfter + attenCredit + time.Minute)
	s.idlePressureSweep(idlePressureDeps{Now: tOv, Running: running})
	if len(ov.events) != 1 {
		t.Fatalf("want 1 overseer event, got %v (repressure=%v)", ov.events, rep.agents)
	}
	if !strings.HasPrefix(ov.events[0], converge.EventClassImpatient+"|") {
		t.Fatalf("class: %q", ov.events[0])
	}
	if !strings.Contains(ov.events[0], "not satisfaction") {
		t.Fatalf("event must state noise is not satisfaction: %q", ov.events[0])
	}

	// Human rung (same credit), then satisfaction via phase=working.
	tHum := now.Add(converge.HumanAlertAfter + attenCredit + time.Minute)
	s.idlePressureSweep(idlePressureDeps{Now: tHum, Running: running})
	if len(hum.raised) != 1 {
		t.Fatalf("want human alert, got %v", hum.raised)
	}

	tWork := tHum.Add(time.Minute)
	activity.by["jv-t317-stuck"] = IdleActivity{Phase: "working", Updated: tWork}
	s.idlePressureSweep(idlePressureDeps{Now: tWork, Running: running})
	eng.mu.Lock()
	openAfter := eng.set.Len()
	trackedAfter := eng.ladder.Tracked("jv-t317-stuck")
	eng.mu.Unlock()
	if openAfter != 0 || trackedAfter {
		t.Fatalf("working must clear set+ladder: open=%d tracked=%v", openAfter, trackedAfter)
	}
	if len(hum.cleared) != 1 || hum.cleared[0] != "jv-t317-stuck" {
		t.Fatalf("satisfaction must withdraw sticky: %v", hum.cleared)
	}
}

// TestIdlePressureSweepWithoutEngineKeepsT315Path ensures hermetic T315
// tests stay on SweepIdleNudges when the ladder is not installed.
func TestIdlePressureSweepWithoutEngineKeepsT315Path(t *testing.T) {
	t.Parallel()
	now := time.Unix(4000, 0)
	s, _ := pressureFixture(t, now)
	pushes := 0
	s.idlePressureSweep(idlePressureDeps{
		Now:     now,
		Running: func(name string) bool { return name == "jv-t315-pressure" },
		Push: func(target, event, text string) error {
			pushes++
			return nil
		},
	})
	if pushes != 1 {
		t.Fatalf("T315 path pushes=%d want 1", pushes)
	}
}
