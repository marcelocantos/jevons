// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package converge

import (
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/converge/attenuate"
)

// 🎯T318 integration hermetics: the pure policy's own oracles live in the
// attenuate subpackage; these check that the ladder actually consumes it.
var attenT0 = time.Date(2026, 8, 8, 11, 14, 0, 0, time.UTC)

func attenAt(d time.Duration) time.Time { return attenT0.Add(d) }

// gapAt builds the single-gap reconcile set 🎯T316 would hand the ladder for
// an agent stuck since attenT0.
func gapAt(satisfied bool) []Gap {
	return []Gap{{Agent: "jv-stuck", Mission: "T999", Since: attenT0, Satisfied: satisfied}}
}

// firedRung returns the rung the ladder fired for jv-stuck, or RungNone.
func firedRung(actions []Action) Rung {
	for _, a := range actions {
		if a.Kind == ActFire && a.Agent == "jv-stuck" {
			return a.Rung
		}
	}
	return RungNone
}

// 🎯T318 acceptance 1: the same gap age reaches a quieter rung once progress
// has been seen — the thresholds genuinely shift out on the live ladder.
func TestAttenuationDelaysTheNextEscalateRung(t *testing.T) {
	// Control: 16m of silence is past OverseerNoiseAfter, so the overseer
	// hears about it.
	bare := NewLadder()
	actions, _ := bare.Reconcile(attenAt(16*time.Minute), gapAt(false))
	if got := firedRung(actions); got != RungOverseerNoise {
		t.Fatalf("unattenuated at 16m: fired %s, want %s", got, RungOverseerNoise)
	}

	// Same 16m, but the overseer restarted the parent at 14m. The ladder
	// drops back to its actuator instead of escalating.
	l := NewLadder()
	l.SetAttenuator(attenuate.NewAttenuator(attenuate.Policy{}))
	l.ObserveProgress("jv-stuck", attenuate.SignalRestart, attenAt(14*time.Minute))

	actions, _ = l.Reconcile(attenAt(16*time.Minute), gapAt(false))
	if got := firedRung(actions); got != RungRepressure {
		t.Errorf("attenuated at 16m: fired %s, want %s — progress must delay the escalate rung", got, RungRepressure)
	}
	adj := l.Attenuation("jv-stuck", attenAt(16*time.Minute))
	if !adj.Attenuated {
		t.Error("Attenuation().Attenuated = false, want true")
	}
	if adj.EffectiveDwell != 8*time.Minute {
		t.Errorf("EffectiveDwell = %s, want 8m (16m less 8m credit)", adj.EffectiveDwell)
	}
}

// 🎯T318 acceptance 4 + the ceiling floor: progress silences the overseer and
// the owner, never the actuator, and never removes the gap.
func TestAttenuationCapsNoiseButKeepsRepressureAndTheGap(t *testing.T) {
	l := NewLadder()
	l.SetAttenuator(attenuate.NewAttenuator(attenuate.Policy{}))

	// Two strong signals: the root restarted the PO and spawned a fix worker.
	l.ObserveProgress("jv-stuck", attenuate.SignalRestart, attenAt(20*time.Minute))
	l.ObserveProgress("jv-stuck", attenuate.SignalSpawn, attenAt(20*time.Minute))

	// 50m dwell is past HumanAlertAfter, but the ceiling is down at the
	// actuator, so that is all that fires.
	actions, closed := l.Reconcile(attenAt(25*time.Minute), gapAt(false))
	if got := firedRung(actions); got != RungRepressure {
		t.Errorf("fired %s, want %s — the ceiling caps noise at the actuator", got, RungRepressure)
	}
	if !l.Tracked("jv-stuck") {
		t.Error("Tracked = false — progress must never remove the gap from the ladder (acceptance 4)")
	}
	if len(closed) != 0 {
		t.Errorf("closed %d incidents, want 0 — progress is not satisfaction", len(closed))
	}
	if adj := l.Attenuation("jv-stuck", attenAt(25*time.Minute)); !adj.GapOpen {
		t.Error("Attenuation().GapOpen = false, want true")
	}
}

// The ceiling is an input to rung choice, not a clamp on its result. A capped
// ladder must still respect the quieter rung's own anti-thrash interval
// instead of firing it every tick.
func TestCeilingDoesNotThrashTheQuieterRung(t *testing.T) {
	l := NewLadder()
	l.SetAttenuator(attenuate.NewAttenuator(attenuate.Policy{}))
	l.ObserveProgress("jv-stuck", attenuate.SignalRestart, attenAt(20*time.Minute))
	l.ObserveProgress("jv-stuck", attenuate.SignalSpawn, attenAt(20*time.Minute))

	actions, _ := l.Reconcile(attenAt(25*time.Minute), gapAt(false))
	if got := firedRung(actions); got != RungRepressure {
		t.Fatalf("first tick fired %s, want %s", got, RungRepressure)
	}
	// 2m later — inside RepressureEvery (5m). Nothing may fire, even though
	// the raw dwell is far past every threshold.
	actions, _ = l.Reconcile(attenAt(27*time.Minute), gapAt(false))
	if got := firedRung(actions); got != RungNone {
		t.Errorf("second tick fired %s, want %s — the capped rung keeps its own interval", got, RungNone)
	}
}

// 🎯T318 acceptance 3: attenuation is temporary. Progress then silence past
// the documented StallBound (12m, attenuate.DefaultPolicy) restores the full
// ceiling and lets impatience climb to the human rung.
func TestStallReClimbsToTheHumanRung(t *testing.T) {
	l := NewLadder()
	l.SetAttenuator(attenuate.NewAttenuator(attenuate.Policy{}))
	l.ObserveProgress("jv-stuck", attenuate.SignalRestart, attenAt(5*time.Minute))

	// Inside the stall bound: quiet holds, actuator only.
	actions, _ := l.Reconcile(attenAt(16*time.Minute), gapAt(false))
	if got := firedRung(actions); got != RungRepressure {
		t.Fatalf("inside StallBound: fired %s, want %s", got, RungRepressure)
	}

	// 50m in, 45m since the last progress signal — well past StallBound and
	// past HumanAlertAfter. Nothing further happened, so the owner hears it.
	actions, _ = l.Reconcile(attenAt(50*time.Minute), gapAt(false))
	if got := firedRung(actions); got != RungHumanAlert {
		t.Errorf("after stall: fired %s, want %s — impatience must climb again", got, RungHumanAlert)
	}
	if adj := l.Attenuation("jv-stuck", attenAt(50*time.Minute)); adj.Credit != 0 {
		t.Errorf("after stall: Credit = %s, want 0 (voided)", adj.Credit)
	}
}

// 🎯T318 acceptance 4 against 🎯T316: only satisfaction closes the gap, and it
// is the close path — not any progress signal — that drops attenuation state.
func TestSatisfactionClosesTheGapAndForgetsAttenuation(t *testing.T) {
	att := attenuate.NewAttenuator(attenuate.Policy{})
	l := NewLadder()
	l.SetAttenuator(att)

	l.ObserveProgress("jv-stuck", attenuate.SignalRestart, attenAt(5*time.Minute))
	l.Reconcile(attenAt(16*time.Minute), gapAt(false))
	if att.State("jv-stuck").Credit == 0 {
		t.Fatal("expected attenuation credit before satisfaction")
	}

	// T316 says the agent is working on its open mission.
	_, closed := l.Reconcile(attenAt(20*time.Minute), gapAt(true))
	if len(closed) != 1 {
		t.Fatalf("closed %d incidents, want 1", len(closed))
	}
	if l.Tracked("jv-stuck") {
		t.Error("Tracked = true after satisfaction, want false")
	}
	if got := att.State("jv-stuck"); got.Credit != 0 || got.Progress != 0 {
		t.Errorf("attenuation state after satisfaction = %+v, want zero — the close path must forget it", got)
	}
}

// A ladder with no attenuator behaves exactly as it did before 🎯T318.
func TestNilAttenuatorIsUnattenuatedTiming(t *testing.T) {
	l := NewLadder()
	// Signals against a ladder with no attenuator are inert, not a panic.
	l.ObserveProgress("jv-stuck", attenuate.SignalRestart, attenAt(time.Minute))

	for _, tc := range []struct {
		at   time.Duration
		want Rung
	}{
		{16 * time.Minute, RungOverseerNoise},
		{50 * time.Minute, RungHumanAlert},
	} {
		actions, _ := l.Reconcile(attenAt(tc.at), gapAt(false))
		if got := firedRung(actions); got != tc.want {
			t.Errorf("nil attenuator at %s: fired %s, want %s", tc.at, got, tc.want)
		}
	}
}

// The ceiling reaches dueRung as a Rung by plain cast. The two orderings must
// not drift apart.
func TestAttenuationCeilingMapsOntoRung(t *testing.T) {
	for _, tc := range []struct {
		c    attenuate.Ceiling
		want Rung
	}{
		{attenuate.CeilingNone, RungNone},
		{attenuate.CeilingRepressure, RungRepressure},
		{attenuate.CeilingOverseer, RungOverseerNoise},
		{attenuate.CeilingHuman, RungHumanAlert},
	} {
		if got := Rung(tc.c); got != tc.want {
			t.Errorf("Rung(%s) = %s, want %s", tc.c, got, tc.want)
		}
		if tc.c.String() != tc.want.String() {
			t.Errorf("names drifted: Ceiling %q vs Rung %q", tc.c.String(), tc.want.String())
		}
	}
}
