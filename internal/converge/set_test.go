// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package converge

import (
	"errors"
	"testing"
	"time"
)

var t0 = time.Date(2026, 8, 8, 11, 14, 0, 0, time.UTC)

func idleObs(name, target string) Observation {
	return Observation{Name: name, Purpose: "work", ProcessRunning: true, Phase: "idle", TargetID: target, MissionOpen: true}
}

// 🎯T316 acceptance 3: enter idle → steps fire → still unsatisfied until the
// agent is working (or the mission closes). This is the whole lifecycle.
func TestGapLifecycleIdleStepsThenWorking(t *testing.T) {
	s := NewSet()
	obs := idleObs("jv-t316-converge", "T316")

	out := s.Reconcile(obs, t0)
	if out.Resolution != ResolutionOpened {
		t.Fatalf("entering idle: resolution %q, want opened", out.Resolution)
	}
	if out.Gap.Episode != 1 || out.Gap.Kind != GapKindIdle {
		t.Fatalf("opened gap = %+v", out.Gap)
	}

	// Step 1: tell the parent PO. Step 2: re-pressure the worker itself.
	now := t0.Add(2 * time.Minute)
	if _, ok := s.RecordStep(out.Key, StepRecord{Kind: StepEventParent, At: now, Delivered: true}); !ok {
		t.Fatal("RecordStep on a standing gap must succeed")
	}
	now = now.Add(5 * time.Minute)
	if _, ok := s.RecordStep(out.Key, StepRecord{Kind: StepRePressure, At: now, Delivered: true}); !ok {
		t.Fatal("RecordStep on a standing gap must succeed")
	}

	// Still idle after both steps: the gap persists, and the observation that
	// keeps it open does not reset the episode.
	out = s.Reconcile(obs, now.Add(time.Minute))
	if out.Resolution != ResolutionPersisting {
		t.Fatalf("still idle after two steps: resolution %q, want persisting", out.Resolution)
	}
	if out.Gap.StepCount() != 2 {
		t.Fatalf("step count = %d, want 2", out.Gap.StepCount())
	}
	if out.Gap.Episode != 1 {
		t.Fatalf("episode = %d, want 1 (same episode)", out.Gap.Episode)
	}
	if s.Len() != 1 {
		t.Fatalf("standing set size = %d, want 1", s.Len())
	}

	// The agent finally takes a turn on the open mission: satisfaction.
	working := obs
	working.Phase = "working"
	out = s.Reconcile(working, now.Add(2*time.Minute))
	if out.Resolution != ResolutionSatisfied || out.Reason != "working_on_open_mission" {
		t.Fatalf("working on open mission: %q/%q, want satisfied/working_on_open_mission", out.Resolution, out.Reason)
	}
	if s.Len() != 0 {
		t.Fatalf("satisfied gap must leave the set; size = %d", s.Len())
	}
	if _, ok := s.Get(out.Key); ok {
		t.Fatal("Get must not return a satisfied gap")
	}
}

// The load-bearing owner pin: having told the PO is a step toward
// satisfaction, not satisfaction. No number of steps of any kind empties
// the standing set.
func TestStepsNeverSatisfyTheGap(t *testing.T) {
	s := NewSet()
	obs := idleObs("jv-idle", "T316")
	out := s.Reconcile(obs, t0)

	kinds := []StepKind{StepEventParent, StepRePressure, StepRehydrate, StepOverseerNoise, StepHumanAlert}
	now := t0
	for i := 0; i < 20; i++ {
		now = now.Add(3 * time.Minute)
		g, ok := s.RecordStep(out.Key, StepRecord{Kind: kinds[i%len(kinds)], At: now, Delivered: true})
		if !ok {
			t.Fatalf("gap disappeared after %d steps — steps must never clear it", i)
		}
		if g.Satisfied() {
			t.Fatal("a gap in the set can never report satisfied")
		}
		if s.Len() != 1 {
			t.Fatalf("set size = %d after %d steps, want 1", s.Len(), i+1)
		}
	}
	// Every rung of the ladder has fired, including the human alert, and the
	// world has not changed: the gap is exactly as open as it was.
	g, ok := s.Get(out.Key)
	if !ok {
		t.Fatal("gap must still stand after the full ladder")
	}
	if g.StepsByKind[StepHumanAlert] == 0 || g.StepsByKind[StepEventParent] == 0 {
		t.Fatalf("step history not recorded: %+v", g.StepsByKind)
	}
	if got := g.Age(now); got != 60*time.Minute {
		t.Fatalf("gap age = %s, want 60m", got)
	}
}

// A terminal report claiming done is prose. Only ledger closure or a real
// reap satisfies — attestation is not execution (🎯T31.1).
func TestClaimsDoneWithoutEvidenceStaysGap(t *testing.T) {
	s := NewSet()
	obs := idleObs("jv-claims", "T316")
	obs.ClaimsDone = true

	out := s.Reconcile(obs, t0)
	if out.Resolution != ResolutionOpened || out.Gap.Kind != GapKindUnverifiedDone {
		t.Fatalf("claims-done idle: %q/%q, want opened/unverified_done", out.Resolution, out.Gap.Kind)
	}
	if _, ok := s.RecordStep(out.Key, StepRecord{Kind: StepEventParent, At: t0.Add(time.Minute), Delivered: true}); !ok {
		t.Fatal("unverified-done gap must accept a step")
	}
	if s.Len() != 1 {
		t.Fatal("telling the parent about an unverified done does not close it")
	}

	// The PO verifies and the ledger closes: now it is satisfied.
	closed := obs
	closed.MissionClosed = true
	out = s.Reconcile(closed, t0.Add(10*time.Minute))
	if out.Resolution != ResolutionSatisfied || out.Reason != "mission_closed" {
		t.Fatalf("ledger closure: %q/%q", out.Resolution, out.Reason)
	}
	if s.Len() != 0 {
		t.Fatal("closed mission must leave the set")
	}
}

// Withdrawal (deliberate stop / design gate) removes the gap without claiming
// anything was achieved, and a reap while the mission is still open reports a
// residual so the successor spawn is not silently lost.
func TestWithdrawnAndResidualAreNotAchievement(t *testing.T) {
	s := NewSet()
	obs := idleObs("jv-parked", "T112")
	s.Reconcile(obs, t0)

	gated := obs
	gated.DesignGated = true
	out := s.Reconcile(gated, t0.Add(time.Minute))
	if out.Resolution != ResolutionWithdrawn || out.Condition == ConditionSatisfied {
		t.Fatalf("design-gated: %q/%q, want withdrawn and not satisfied", out.Resolution, out.Condition)
	}
	if _, ok := out.LadderView(); ok {
		t.Fatal("withdrawal owes no postmortem — it must not present as satisfaction")
	}

	s.Reconcile(idleObs("jv-reaped", "T316"), t0)
	reaped := idleObs("jv-reaped", "T316")
	reaped.Reaped = true
	out = s.Reconcile(reaped, t0.Add(time.Minute))
	if out.Resolution != ResolutionSatisfied {
		t.Fatalf("reap: %q, want satisfied", out.Resolution)
	}
	if !out.ResidualMissionOpen {
		t.Fatal("reaping an agent off a still-open mission must report the residual")
	}
}

// Flapping is visible: a worker that goes working then idle again opens a new
// episode rather than resuming the old one, and the count survives so the
// ladder (🎯T317) and attenuation (🎯T318) can see the pattern.
func TestReopenAfterFlapCountsEpisodes(t *testing.T) {
	s := NewSet()
	idle := idleObs("jv-flap", "T316")
	working := idle
	working.Phase = "working"

	s.Reconcile(idle, t0)
	s.Reconcile(working, t0.Add(time.Minute))
	out := s.Reconcile(idle, t0.Add(2*time.Minute))
	if out.Resolution != ResolutionOpened {
		t.Fatalf("re-entering idle: %q, want opened", out.Resolution)
	}
	if out.Gap.Episode != 2 {
		t.Fatalf("episode = %d, want 2", out.Gap.Episode)
	}
	if out.Gap.StepCount() != 0 {
		t.Fatal("a new episode starts with a clean step history")
	}

	// Re-binding to a different mission is also a new episode: the old gap's
	// step history must not be credited to the new mission.
	rebound := idle
	rebound.TargetID = "T317"
	out = s.Reconcile(rebound, t0.Add(3*time.Minute))
	if out.Resolution != ResolutionOpened || out.Gap.Mission != "T317" || out.Gap.Episode != 3 {
		t.Fatalf("re-bound gap = %+v", out.Gap)
	}
	if s.Len() != 1 {
		t.Fatalf("one agent holds one gap; size = %d", s.Len())
	}
}

// Drive is the integration point 🎯T315 and 🎯T317 plug into: it offers each
// standing gap to an actuator subject to a due policy, records what came back,
// and leaves membership untouched.
func TestDriveRecordsStepsAndKeepsGapsStanding(t *testing.T) {
	s := NewSet()
	s.Reconcile(idleObs("jv-a", "T316"), t0)
	s.Reconcile(idleObs("jv-b", "T315"), t0)

	calls := 0
	act := ActuatorFunc(func(g MissionGap, now time.Time) (StepKind, error) {
		calls++
		if g.Agent == "jv-b" {
			return StepRePressure, errors.New("deliver failed: no session")
		}
		return StepRePressure, nil
	})

	recs := s.Drive(t0.Add(3*time.Minute), DefaultDue(DefaultMinStepInterval), act)
	if calls != 2 || len(recs) != 2 {
		t.Fatalf("calls=%d records=%d, want 2/2", calls, len(recs))
	}
	if s.Len() != 2 {
		t.Fatal("driving the actuator must not change set membership")
	}
	var failed int
	for _, r := range recs {
		if !r.Delivered {
			failed++
			if r.Err == "" {
				t.Fatal("a failed step must carry its error")
			}
		}
	}
	if failed != 1 {
		t.Fatalf("failed steps = %d, want 1", failed)
	}

	// Backoff: another tick inside the minimum interval offers nothing.
	calls = 0
	if recs := s.Drive(t0.Add(4*time.Minute), DefaultDue(DefaultMinStepInterval), act); len(recs) != 0 || calls != 0 {
		t.Fatalf("due policy ignored: calls=%d records=%d", calls, len(recs))
	}

	// An actuator with nothing to do records nothing.
	quiet := ActuatorFunc(func(MissionGap, time.Time) (StepKind, error) { return "", nil })
	if recs := s.Drive(t0.Add(30*time.Minute), DefaultDue(DefaultMinStepInterval), quiet); len(recs) != 0 {
		t.Fatalf("quiet actuator recorded %d steps", len(recs))
	}
	if s.Len() != 2 {
		t.Fatal("gaps must still stand after a quiet tick")
	}
}

// The bridge to the landed 🎯T317 ladder: standing gaps present as
// unsatisfied, and satisfaction is reported once, on the tick it happens.
func TestLadderViewBridge(t *testing.T) {
	s := NewSet()
	out := s.Reconcile(idleObs("jv-t316-converge", "T316"), t0)

	set := s.LadderGaps()
	if len(set) != 1 {
		t.Fatalf("ladder view size = %d, want 1", len(set))
	}
	if set[0].Agent != "jv-t316-converge" || set[0].Mission != "T316" {
		t.Fatalf("ladder view = %+v", set[0])
	}
	if set[0].Satisfied {
		t.Fatal("a standing gap must never present to the ladder as satisfied")
	}
	if !set[0].Since.Equal(t0) {
		t.Fatalf("Since = %s, want the moment the gap opened (%s)", set[0].Since, t0)
	}

	// A fired human-alert rung changes nothing about the view.
	s.RecordStep(out.Key, StepRecord{Kind: StepHumanAlert, At: t0.Add(time.Hour), Delivered: true})
	if set := s.LadderGaps(); len(set) != 1 || set[0].Satisfied {
		t.Fatalf("after the loudest rung the ladder still sees an unsatisfied gap: %+v", set)
	}

	working := idleObs("jv-t316-converge", "T316")
	working.Phase = "working"
	out = s.Reconcile(working, t0.Add(2*time.Hour))
	view, ok := out.LadderView()
	if !ok || !view.Satisfied || view.Agent != "jv-t316-converge" {
		t.Fatalf("satisfaction view = %+v (ok=%v)", view, ok)
	}
	if len(s.LadderGaps()) != 0 {
		t.Fatal("satisfied gap must vanish from the ladder's set")
	}
}
