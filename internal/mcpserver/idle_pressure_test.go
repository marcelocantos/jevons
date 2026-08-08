// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T315: the daemon re-pressures open-mission phase=idle workers itself.
// Event-first to the parent PO stays, but a PO that is idle or queued must not
// be the only path — that left implementers silent for hours.

// pressureFixture builds a server with one overseer, one PO, and one
// open-mission worker that has been idle past the threshold.
func pressureFixture(t *testing.T, now time.Time) (*Server, *IdleActivityTracker) {
	t.Helper()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{
		{Name: "jevons", WorkDir: dir, SessionID: "s-o", Purpose: claudia.PurposeOverseer,
			Materialized: true, Provider: "grok", AutoStart: true},
		{Name: "jv-t315-pressure", WorkDir: dir, SessionID: "s-w", Purpose: claudia.PurposeWork,
			Parent: "jevons-po", Materialized: true, Provider: "grok", AutoStart: true, TargetID: "T315"},
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
	activity.by["jv-t315-pressure"] = IdleActivity{
		Phase:   "idle",
		Updated: now.Add(-DefaultIdleNudgeThreshold - time.Minute),
	}
	return &Server{registry: reg, idleActivity: activity, idleNudgeLedger: ledger}, activity
}

func TestIdlePressureSweepDeliversWithoutOverseerTurn(t *testing.T) {
	t.Parallel()
	now := time.Unix(3000, 0)
	s, _ := pressureFixture(t, now)

	var pushed []struct{ target, event, text string }
	reps := s.idlePressureSweep(idlePressureDeps{
		Now:     now,
		Running: func(name string) bool { return name == "jv-t315-pressure" },
		Push: func(target, event, text string) error {
			pushed = append(pushed, struct{ target, event, text string }{target, event, text})
			return nil
		},
	})

	if len(pushed) != 1 || pushed[0].target != "jv-t315-pressure" {
		t.Fatalf("want exactly one deliver to the idle worker, got %+v", pushed)
	}
	// Real brief-or-continue body, not a bare event to a parent.
	if !strings.Contains(pushed[0].text, "Jevons fleet standing brief") {
		t.Fatalf("first pass must carry the full brief: %q", pushed[0].text)
	}
	if !strings.Contains(pushed[0].event, "idle-nudge") {
		t.Fatalf("event=%q want an idle-nudge source", pushed[0].event)
	}
	// No overseer turn was needed, and the overseer itself is never nudged.
	for _, p := range pushed {
		if p.target == "jevons" {
			t.Fatalf("overseer must not be in the pressure path: %+v", p)
		}
	}
	var w IdleNudgeReport
	for _, r := range reps {
		if r.Name == "jv-t315-pressure" {
			w = r
		}
	}
	if !w.Delivered || w.Reason != "idle_stuck" {
		t.Fatalf("worker report=%+v want delivered idle_stuck", w)
	}
}

func TestIdlePressureSweepBackoffThenMaxed(t *testing.T) {
	t.Parallel()
	now := time.Unix(3000, 0)
	s, activity := pressureFixture(t, now)
	running := func(name string) bool { return name == "jv-t315-pressure" }

	pushes := 0
	push := func(target, event, text string) error { pushes++; return nil }

	s.idlePressureSweep(idlePressureDeps{Now: now, Running: running, Push: push})
	if pushes != 1 {
		t.Fatalf("first sweep pushes=%d want 1", pushes)
	}

	// Still idle a few seconds later: backoff must suppress the re-send.
	soon := now.Add(10 * time.Second)
	activity.by["jv-t315-pressure"] = IdleActivity{Phase: "idle", Updated: now.Add(-time.Hour)}
	reps := s.idlePressureSweep(idlePressureDeps{Now: soon, Running: running, Push: push})
	if pushes != 1 {
		t.Fatalf("backoff sweep pushes=%d want 1 (no thrash)", pushes)
	}
	if r := reportFor(reps, "jv-t315-pressure"); r.Reason != "backoff" {
		t.Fatalf("want backoff skip, got %+v", r)
	}

	// Past the first backoff, pressure resumes.
	later := now.Add(3 * time.Minute)
	reps = s.idlePressureSweep(idlePressureDeps{Now: later, Running: running, Push: push})
	if pushes != 2 {
		t.Fatalf("post-backoff pushes=%d want 2", pushes)
	}

	// Exhaust the budget: the actuator stops rather than looping forever, and
	// hands the agent to the escalation hook (🎯T317 seam).
	for {
		c, _ := s.idleNudgeLedger.Get("jv-t315-pressure")
		if c >= DefaultIdleNudgeMax {
			break
		}
		if err := s.idleNudgeLedger.Record("jv-t315-pressure", later); err != nil {
			t.Fatal(err)
		}
	}
	var maxed []string
	s.SetIdlePressureHooks(IdlePressureHooks{
		OnMaxed: func(rep IdleNudgeReport) { maxed = append(maxed, rep.Name) },
	})
	far := later.Add(2 * time.Hour)
	activity.by["jv-t315-pressure"] = IdleActivity{Phase: "idle", Updated: far.Add(-time.Hour)}
	reps = s.idlePressureSweep(idlePressureDeps{Now: far, Running: running, Push: push})
	if pushes != 2 {
		t.Fatalf("maxed sweep pushes=%d want 2 (no infinite ladder)", pushes)
	}
	if r := reportFor(reps, "jv-t315-pressure"); r.Action != IdleNudgeMaxed {
		t.Fatalf("want maxed, got %+v", r)
	}
	if len(maxed) != 1 || maxed[0] != "jv-t315-pressure" {
		t.Fatalf("OnMaxed escalation hook not called: %v", maxed)
	}
}

// A worker whose mission the satisfaction layer (🎯T316) closed is left alone.
func TestIdlePressureSweepRespectsMissionOpenHook(t *testing.T) {
	t.Parallel()
	now := time.Unix(3000, 0)
	s, _ := pressureFixture(t, now)
	s.SetIdlePressureHooks(IdlePressureHooks{
		MissionOpen: func(targetID string) bool { return targetID != "T315" },
	})

	pushes := 0
	reps := s.idlePressureSweep(idlePressureDeps{
		Now:     now,
		Running: func(name string) bool { return true },
		Push:    func(target, event, text string) error { pushes++; return nil },
	})
	if pushes != 0 {
		t.Fatalf("closed mission must not be re-pressured (pushes=%d)", pushes)
	}
	if r := reportFor(reps, "jv-t315-pressure"); r.Reason != "no_open_mission" {
		t.Fatalf("want no_open_mission skip, got %+v", r)
	}
}

func TestRunIdlePressureLoopTicks(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan struct{}, 4)
	go runIdlePressureLoop(ctx, time.Millisecond, func() {
		select {
		case ticks <- struct{}{}:
		default:
		}
	})
	for i := 0; i < 2; i++ {
		select {
		case <-ticks:
		case <-time.After(2 * time.Second):
			t.Fatal("periodic actuator never ticked")
		}
	}
}

func reportFor(reps []IdleNudgeReport, name string) IdleNudgeReport {
	for _, r := range reps {
		if r.Name == name {
			return r
		}
	}
	return IdleNudgeReport{}
}
