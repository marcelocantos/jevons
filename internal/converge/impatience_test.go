// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package converge

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 8, 8, 9, 0, 0, 0, time.UTC)

func idle(agent string) []Gap {
	return []Gap{{Agent: agent, Mission: "T317", Since: t0}}
}

func fired(actions []Action) []Rung {
	var out []Rung
	for _, a := range actions {
		if a.Kind == ActFire {
			out = append(out, a.Rung)
		}
	}
	return out
}

// The ladder walks re-pressure → overseer noise → human alert on the
// documented bounds, and stays silent inside the grace period (🎯T317 (1)).
func TestLadderClimbsOnDocumentedBounds(t *testing.T) {
	l := NewLadder()

	if acts, _ := l.Reconcile(t0.Add(RepressureAfter-time.Minute), idle("jv-x")); len(acts) != 0 {
		t.Fatalf("fired inside grace period: %+v", acts)
	}
	for _, tc := range []struct {
		dwell time.Duration
		want  Rung
	}{
		{RepressureAfter, RungRepressure},
		{OverseerNoiseAfter, RungOverseerNoise},
		{HumanAlertAfter, RungHumanAlert},
	} {
		acts, closed := l.Reconcile(t0.Add(tc.dwell), idle("jv-x"))
		got := fired(acts)
		if len(got) != 1 || got[0] != tc.want {
			t.Fatalf("dwell %v: got %v, want [%v]", tc.dwell, got, tc.want)
		}
		if len(closed) != 0 {
			t.Fatalf("dwell %v: unexpected incident close %+v", tc.dwell, closed)
		}
	}
}

// Firing any rung — the overseer event included — leaves the gap tracked
// and unsatisfied (🎯T317 (2): noise is not satisfaction).
func TestOverseerNoiseDoesNotSatisfyGap(t *testing.T) {
	l := NewLadder()
	acts, _ := l.Reconcile(t0.Add(OverseerNoiseAfter), idle("jv-x"))
	got := fired(acts)
	if len(got) != 1 || got[0] != RungOverseerNoise {
		t.Fatalf("got %v, want [overseer-noise]", got)
	}
	if acts[0].Satisfies {
		t.Fatal("overseer noise reported itself as satisfaction")
	}
	if !l.Tracked("jv-x") {
		t.Fatal("gap dropped from the reconcile set after noise")
	}
	// Still unsatisfied on the next tick: the ladder keeps escalating.
	acts, _ = l.Reconcile(t0.Add(HumanAlertAfter), idle("jv-x"))
	if got := fired(acts); len(got) != 1 || got[0] != RungHumanAlert {
		t.Fatalf("ladder stalled after noise: %v", got)
	}
}

// Anti-thrash: a fast converge loop gets one action per agent per tick and
// no repeat inside the rung's interval (🎯T317 (4)).
func TestAntiThrashCoalescesPerAgent(t *testing.T) {
	l := NewLadder()
	l.Reconcile(t0.Add(RepressureAfter), idle("jv-x"))

	for i := 1; i <= 4; i++ { // 15s ticks, still inside RepressureEvery
		at := t0.Add(RepressureAfter + time.Duration(i)*15*time.Second)
		if acts, _ := l.Reconcile(at, idle("jv-x")); len(acts) != 0 {
			t.Fatalf("tick %d spammed: %+v", i, acts)
		}
	}
	acts, _ := l.Reconcile(t0.Add(RepressureAfter+RepressureEvery), idle("jv-x"))
	if got := fired(acts); len(got) != 1 || got[0] != RungRepressure {
		t.Fatalf("re-pressure did not repeat after its interval: %v", got)
	}
	// Well past every threshold, one tick still fires only the loudest rung.
	acts, _ = l.Reconcile(t0.Add(HumanAlertAfter+time.Hour), idle("jv-x"))
	if got := fired(acts); len(got) != 1 || got[0] != RungHumanAlert {
		t.Fatalf("multi-rung tick, got %v", got)
	}
}

// True satisfaction clears the human sticky with no ack, and hands 🎯T319 a
// closed incident with the rungs that fired (🎯T319 (1)(3)).
func TestSatisfactionClearsHumanAlertWithoutAck(t *testing.T) {
	l := NewLadder()
	l.Reconcile(t0.Add(RepressureAfter), idle("jv-x"))
	l.Reconcile(t0.Add(HumanAlertAfter), idle("jv-x"))

	at := t0.Add(HumanAlertAfter + time.Minute)
	acts, closed := l.Reconcile(at, []Gap{{Agent: "jv-x", Mission: "T317", Since: t0, Satisfied: true}})
	if len(acts) != 1 || acts[0].Kind != ActClearHuman {
		t.Fatalf("satisfaction did not clear the sticky: %+v", acts)
	}
	if l.Tracked("jv-x") {
		t.Fatal("satisfied gap still tracked")
	}
	if len(closed) != 1 {
		t.Fatalf("want 1 closed incident, got %d", len(closed))
	}
	inc := closed[0]
	if !inc.HumanLit || inc.Acked {
		t.Fatalf("incident record wrong: %+v", inc)
	}
	if len(inc.Rungs) != 2 || inc.Rungs[0] != RungRepressure || inc.Rungs[1] != RungHumanAlert {
		t.Fatalf("rung history wrong: %v", inc.Rungs)
	}
	if inc.Dwell != at.Sub(t0) {
		t.Fatalf("dwell %v, want %v", inc.Dwell, at.Sub(t0))
	}
}

// A gap that vanishes from the reconcile set counts as satisfied: same
// clear, same incident close, whatever the resolution pathway (🎯T319 (3)).
func TestGapLeavingSetClosesIncident(t *testing.T) {
	l := NewLadder()
	l.Reconcile(t0.Add(HumanAlertAfter), idle("jv-x"))
	acts, closed := l.Reconcile(t0.Add(HumanAlertAfter+time.Minute), nil)
	if len(acts) != 1 || acts[0].Kind != ActClearHuman {
		t.Fatalf("want a clear action, got %+v", acts)
	}
	if len(closed) != 1 || closed[0].Agent != "jv-x" {
		t.Fatalf("want closed incident for jv-x, got %+v", closed)
	}
}

// A gap resolved before any rung fired is not an incident — nothing to
// postmortem, nothing to clear.
func TestQuietResolutionIsNotAnIncident(t *testing.T) {
	l := NewLadder()
	l.Reconcile(t0.Add(time.Minute), idle("jv-x"))
	acts, closed := l.Reconcile(t0.Add(2*time.Minute), nil)
	if len(acts) != 0 || len(closed) != 0 {
		t.Fatalf("quiet resolution produced noise: %+v %+v", acts, closed)
	}
}

// Ack silences the human rung early but never clears the gap or stops the
// lower rungs (🎯T319 (2)).
func TestAckSilencesHumanRungOnly(t *testing.T) {
	l := NewLadder()
	l.Reconcile(t0.Add(HumanAlertAfter), idle("jv-x"))
	l.Ack("jv-x")

	acts, _ := l.Reconcile(t0.Add(HumanAlertAfter+HumanAlertEvery), idle("jv-x"))
	for _, a := range acts {
		if a.Rung == RungHumanAlert && a.Kind == ActFire {
			t.Fatalf("human rung re-fired after ack: %+v", a)
		}
	}
	if !l.Tracked("jv-x") {
		t.Fatal("ack cleared the gap; ack is not satisfaction")
	}
	_, closed := l.Reconcile(t0.Add(HumanAlertAfter+time.Hour), nil)
	if len(closed) != 1 || !closed[0].Acked {
		t.Fatalf("acked incident not reported to the postmortem path: %+v", closed)
	}
}

// Escalation state is per agent: one loud gap does not throttle another.
func TestPerAgentIndependence(t *testing.T) {
	l := NewLadder()
	l.Reconcile(t0.Add(RepressureAfter), idle("jv-x"))
	set := []Gap{
		{Agent: "jv-x", Mission: "T317", Since: t0},
		{Agent: "jv-y", Mission: "T318", Since: t0.Add(OverseerNoiseAfter)},
	}
	acts, _ := l.Reconcile(t0.Add(OverseerNoiseAfter+RepressureAfter), set)
	if len(acts) != 2 {
		t.Fatalf("want one action per agent, got %+v", acts)
	}
	if acts[0].Agent != "jv-x" || acts[0].Rung != RungOverseerNoise {
		t.Fatalf("jv-x action wrong: %+v", acts[0])
	}
	if acts[1].Agent != "jv-y" || acts[1].Rung != RungRepressure {
		t.Fatalf("jv-y action wrong: %+v", acts[1])
	}
}
