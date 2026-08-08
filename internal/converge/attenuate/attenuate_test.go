// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package attenuate

import (
	"testing"
	"time"
)

// t0 is an arbitrary fixed clock. Every test drives time explicitly: the
// policy is pure, so no test may consult the wall clock.
var t0 = time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)

func at(d time.Duration) time.Time { return t0.Add(d) }

// 🎯T318 acceptance 1 + 5: impatience is multi-factor. The same gap age
// yields a later effective dwell and a lower noise ceiling once real progress
// has been seen — so 🎯T317's thresholds shift out and its loud rungs are
// capped.
func TestProgressLowersCeilingAndDelaysNextRung(t *testing.T) {
	p := DefaultPolicy()

	// Control: age alone, no signals. Full ladder, full timing.
	_, bare := p.Adjust(State{}, 20*time.Minute, at(20*time.Minute))
	if bare.EffectiveDwell != 20*time.Minute {
		t.Errorf("no signals: EffectiveDwell = %s, want the raw 20m", bare.EffectiveDwell)
	}
	if bare.Ceiling != CeilingHuman {
		t.Errorf("no signals: Ceiling = %s, want %s", bare.Ceiling, CeilingHuman)
	}
	if bare.Attenuated {
		t.Error("no signals: Attenuated = true, want false")
	}

	// Same gap age, but the overseer restarted the parent and spawned a fix
	// worker at 18m.
	s := State{}
	s = p.Observe(s, Signal{Kind: SignalRestart, At: at(18 * time.Minute)}, at(18*time.Minute))
	s = p.Observe(s, Signal{Kind: SignalSpawn, At: at(18 * time.Minute)}, at(18*time.Minute))
	s, adj := p.Adjust(s, 20*time.Minute, at(20*time.Minute))

	if adj.EffectiveDwell >= bare.EffectiveDwell {
		t.Errorf("progress: EffectiveDwell = %s, want less than the bare %s", adj.EffectiveDwell, bare.EffectiveDwell)
	}
	if want := 20*time.Minute - 16*time.Minute; adj.EffectiveDwell != want {
		t.Errorf("progress: EffectiveDwell = %s, want %s (20m less 2 × 8m credit)", adj.EffectiveDwell, want)
	}
	if adj.Ceiling != CeilingRepressure {
		t.Errorf("progress: Ceiling = %s, want %s (two strong signals step down from human)", adj.Ceiling, CeilingRepressure)
	}
	if !adj.Attenuated {
		t.Error("progress: Attenuated = false, want true")
	}
	if s.Strong != 2 {
		t.Errorf("Strong = %d, want 2", s.Strong)
	}
}

// 🎯T318 acceptance 3 + 5: attenuation is temporary. With no further progress
// inside StallBound the credit is void, the ceiling is restored to the human
// rung, and impatience climbs again.
func TestStallVoidsCreditAndReClimbs(t *testing.T) {
	p := DefaultPolicy()

	s := p.Observe(State{}, Signal{Kind: SignalRestart, At: at(10 * time.Minute)}, at(10*time.Minute))
	s, held := p.Adjust(s, 12*time.Minute, at(12*time.Minute))
	if !held.Attenuated || held.Credit != 8*time.Minute {
		t.Fatalf("before stall: Attenuated = %v, Credit = %s, want true / 8m", held.Attenuated, held.Credit)
	}
	if held.Ceiling != CeilingOverseer {
		t.Fatalf("before stall: Ceiling = %s, want %s", held.Ceiling, CeilingOverseer)
	}

	// StallBound past the last progress signal: nothing further has happened.
	s, lapsed := p.Adjust(s, 23*time.Minute, at(23*time.Minute))
	if !lapsed.Stalled {
		t.Error("after StallBound: Stalled = false, want true")
	}
	if lapsed.Credit != 0 {
		t.Errorf("after StallBound: Credit = %s, want 0 (voided)", lapsed.Credit)
	}
	if lapsed.EffectiveDwell != 23*time.Minute {
		t.Errorf("after StallBound: EffectiveDwell = %s, want the full 23m", lapsed.EffectiveDwell)
	}
	if lapsed.Ceiling != CeilingHuman {
		t.Errorf("after StallBound: Ceiling = %s, want %s (full ladder restored)", lapsed.Ceiling, CeilingHuman)
	}
	if s.Strikes != 1 {
		t.Errorf("Strikes = %d, want 1", s.Strikes)
	}

	// One quiet stretch is one strike however many ticks observe it.
	s, _ = p.Adjust(s, 30*time.Minute, at(30*time.Minute))
	if s.Strikes != 1 {
		t.Errorf("Strikes after a second stalled tick = %d, want 1", s.Strikes)
	}
}

// 🎯T318 acceptance 4: progress is never satisfaction. No sequence of signals
// can close the gap — only 🎯T316's verdict does, and that verdict is not an
// input to this package.
func TestProgressNeverClearsTheGap(t *testing.T) {
	p := DefaultPolicy()
	s := State{}
	kinds := []SignalKind{SignalParentWorking, SignalRestart, SignalSpawn, SignalTargetWorking, SignalRepressureDelivered}

	for i, k := range kinds {
		when := at(time.Duration(i) * time.Minute)
		s = p.Observe(s, Signal{Kind: k, At: when}, when)
		var adj Adjustment
		s, adj = p.Adjust(s, time.Duration(i)*time.Minute, when)
		if !adj.GapOpen {
			t.Fatalf("%s: GapOpen = false — attenuation must never close a gap", k)
		}
		if adj.Ceiling < CeilingRepressure {
			t.Fatalf("%s: Ceiling = %s, want at least %s — the actuator is never silenced", k, adj.Ceiling, CeilingRepressure)
		}
	}
}

// 🎯T318 acceptance 2: notify-only and empty ack are NOT progress. Telling
// somebody buys nothing.
func TestNotifyOnlyAndEmptyAckAreNotProgress(t *testing.T) {
	p := DefaultPolicy()
	s := State{}
	for _, k := range []SignalKind{SignalNotifyOnly, SignalEmptyAck, SignalUnknown} {
		if k.IsProgress() {
			t.Errorf("%s: IsProgress() = true, want false", k)
		}
		s = p.Observe(s, Signal{Kind: k, At: at(time.Minute)}, at(time.Minute))
	}
	_, adj := p.Adjust(s, 20*time.Minute, at(20*time.Minute))
	if adj.Credit != 0 {
		t.Errorf("Credit = %s, want 0 — notify/ack buy no delay", adj.Credit)
	}
	if adj.EffectiveDwell != 20*time.Minute {
		t.Errorf("EffectiveDwell = %s, want the full 20m", adj.EffectiveDwell)
	}
	if adj.Ceiling != CeilingHuman {
		t.Errorf("Ceiling = %s, want %s — notify/ack never quieten the ladder", adj.Ceiling, CeilingHuman)
	}
	if s.Progress != 0 {
		t.Errorf("Progress = %d, want 0", s.Progress)
	}
}

// 🎯T318 acceptance 1 + 3: attenuation never freezes the ladder. An unbroken
// drip of real progress signals still reaches the human rung, because credit
// is capped per gap and over the gap's life.
func TestAttenuationIsBoundedSoHumanRungStillArrives(t *testing.T) {
	p := DefaultPolicy()
	s := State{}

	// A progress signal every two minutes for six hours — never a stall, so
	// credit is never voided by the stall path. Only the caps hold it down.
	var last Adjustment
	for m := 2; m <= 360; m += 2 {
		when := at(time.Duration(m) * time.Minute)
		s = p.Observe(s, Signal{Kind: SignalParentWorking, At: when}, when)
		s, last = p.Adjust(s, time.Duration(m)*time.Minute, when)
		if s.Credit > p.MaxCredit {
			t.Fatalf("minute %d: Credit = %s, exceeds MaxCredit %s", m, s.Credit, p.MaxCredit)
		}
		if s.Granted > p.MaxLifetimeCredit {
			t.Fatalf("minute %d: Granted = %s, exceeds MaxLifetimeCredit %s", m, s.Granted, p.MaxLifetimeCredit)
		}
	}

	// 🎯T317 raises the human rung at 45m of effective dwell. Six hours of
	// continuous progress must not have held it off.
	const humanAlertAfter = 45 * time.Minute
	if last.EffectiveDwell < humanAlertAfter {
		t.Errorf("EffectiveDwell after 6h of unbroken progress = %s, want at least the human threshold %s — attenuation must not freeze the ladder", last.EffectiveDwell, humanAlertAfter)
	}
	if last.Ceiling < CeilingRepressure {
		t.Errorf("Ceiling = %s, want at least %s", last.Ceiling, CeilingRepressure)
	}
}

// 🎯T318 acceptance 2: the engine's own re-pressure deliver is weak progress.
// It buys delay — something did land on the agent — but it must not lower the
// noise ceiling, or the engine would be scoring its own noise as the cure.
func TestEngineOwnRepressureBuysDelayButNoDeEscalation(t *testing.T) {
	p := DefaultPolicy()

	s := p.Observe(State{}, Signal{Kind: SignalRepressureDelivered, At: at(5 * time.Minute)}, at(5*time.Minute))
	_, adj := p.Adjust(s, 6*time.Minute, at(6*time.Minute))

	if adj.Credit != p.Credit {
		t.Errorf("Credit = %s, want %s — a landed deliver still buys delay", adj.Credit, p.Credit)
	}
	if adj.Ceiling != CeilingHuman {
		t.Errorf("Ceiling = %s, want %s — the engine's own action never quietens the ladder", adj.Ceiling, CeilingHuman)
	}
	if SignalRepressureDelivered.IsStrong() {
		t.Error("SignalRepressureDelivered.IsStrong() = true, want false")
	}
}

// 🎯T318 acceptance 3: progress that never converges buys less and less.
// Each stall strike halves the next signal's credit, floored at MinCredit.
func TestStallStrikesDiminishCredit(t *testing.T) {
	p := DefaultPolicy()
	s := State{}
	var granted []time.Duration

	// Four cycles of "one progress signal, then silence past StallBound".
	for cycle := range 4 {
		base := time.Duration(cycle) * time.Hour
		when := at(base + time.Minute)
		before := s.Granted
		s = p.Observe(s, Signal{Kind: SignalSpawn, At: when}, when)
		granted = append(granted, s.Granted-before)
		s, _ = p.Adjust(s, base+30*time.Minute, at(base+30*time.Minute)) // stall
	}

	for i := 1; i < len(granted); i++ {
		if granted[i] > granted[i-1] {
			t.Errorf("cycle %d bought %s, more than cycle %d's %s — credit must not grow across stalls", i, granted[i], i-1, granted[i-1])
		}
	}
	if granted[0] != p.Credit {
		t.Errorf("first signal bought %s, want the full %s", granted[0], p.Credit)
	}
	if last := granted[len(granted)-1]; last < p.MinCredit {
		t.Errorf("last signal bought %s, want at least MinCredit %s", last, p.MinCredit)
	}
	if granted[len(granted)-1] >= granted[0] {
		t.Errorf("credit did not diminish: first %s, last %s", granted[0], granted[len(granted)-1])
	}
}

// 🎯T318 acceptance 4 boundary: however much progress lands, the ceiling
// floors at the re-pressure rung. The gap is open, so the actuator keeps
// firing.
func TestCeilingFloorsAtRepressure(t *testing.T) {
	p := DefaultPolicy()
	s := State{}
	for i := range 10 {
		when := at(time.Duration(i) * time.Minute)
		s = p.Observe(s, Signal{Kind: SignalTargetWorking, At: when}, when)
	}
	if s.Ceiling != CeilingRepressure {
		t.Errorf("Ceiling after 10 strong signals = %s, want %s", s.Ceiling, CeilingRepressure)
	}
	if s.Ceiling == CeilingNone {
		t.Error("Ceiling reached none — an open gap always keeps its actuator")
	}
}

// The Ceiling ordinals are the consumer seam: 🎯T317 clamps with a plain cast
// to converge.Rung, so the two orderings must not drift apart.
func TestCeilingOrdinalsMirrorLadderRungs(t *testing.T) {
	for _, tc := range []struct {
		c    Ceiling
		want int
		name string
	}{
		{CeilingNone, 0, "none"},
		{CeilingRepressure, 1, "repressure"},
		{CeilingOverseer, 2, "overseer-noise"},
		{CeilingHuman, 3, "human-alert"},
	} {
		if int(tc.c) != tc.want {
			t.Errorf("%s ordinal = %d, want %d (must match converge.Rung)", tc.name, int(tc.c), tc.want)
		}
		if tc.c.String() != tc.name {
			t.Errorf("Ceiling(%d).String() = %q, want %q", tc.want, tc.c.String(), tc.name)
		}
	}
}

// The Attenuator keeps per-agent state: one agent's progress must not quieten
// another agent's gap.
func TestAttenuatorIsolatesAgents(t *testing.T) {
	a := NewAttenuator(Policy{})

	a.Observe("jv-busy", Signal{Kind: SignalRestart, At: at(time.Minute)}, at(time.Minute))
	busy := a.Adjust("jv-busy", 20*time.Minute, at(2*time.Minute))
	stuck := a.Adjust("jv-stuck", 20*time.Minute, at(2*time.Minute))

	if !busy.Attenuated {
		t.Error("jv-busy: Attenuated = false, want true")
	}
	if stuck.Attenuated {
		t.Error("jv-stuck: Attenuated = true — another agent's progress must not quieten this gap")
	}
	if stuck.EffectiveDwell != 20*time.Minute {
		t.Errorf("jv-stuck: EffectiveDwell = %s, want the full 20m", stuck.EffectiveDwell)
	}

	// Forget is for satisfaction only, and it resets to the full ladder.
	a.Forget("jv-busy")
	if got := a.Adjust("jv-busy", 20*time.Minute, at(3*time.Minute)); got.Attenuated {
		t.Error("after Forget: Attenuated = true, want false")
	}
}
