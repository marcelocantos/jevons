// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package converge

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/agenterr"
)

// 🎯T454: fixture turn outputs drive satisfaction / rung suppression.

func TestT454RefusalOnlyTurnDoesNotSatisfy(t *testing.T) {
	spend := "You've hit your monthly spend limit. Run /usage-credits to manage your limit."
	if agenterr.ClassifyTurnOutput(spend) != agenterr.TurnRefusalOnly {
		t.Fatal("fixture must classify as refusal-only")
	}
	o := Observation{
		Name: "jv-x", Purpose: "work", ProcessRunning: true,
		TargetID: "T454", MissionOpen: true,
		Phase: "working", RefusalHold: true,
	}
	cond, _, why := ClassifyObservation(o)
	if cond != ConditionGap || why != "refusal_only_turn" {
		t.Fatalf("got cond=%s why=%s, want gap/refusal_only_turn", cond, why)
	}
}

func TestT454SubstantiveTurnCloses(t *testing.T) {
	work := "Done — implemented the refusal-hold latch and gated the suite."
	if agenterr.ClassifyTurnOutput(work) != agenterr.TurnSubstantive {
		t.Fatal("fixture must classify as substantive")
	}
	o := Observation{
		Name: "jv-x", Purpose: "work", ProcessRunning: true,
		TargetID: "T454", MissionOpen: true,
		Phase: "idle", SubstantiveTurn: true,
	}
	cond, _, why := ClassifyObservation(o)
	if cond != ConditionSatisfied || why != "substantive_turn" {
		t.Fatalf("got cond=%s why=%s, want satisfied/substantive_turn", cond, why)
	}
}

func TestT454MixedTurnCloses(t *testing.T) {
	mixed := "Hit the spend limit earlier; I've started on T454 and wrote the classifier."
	if agenterr.ClassifyTurnOutput(mixed) != agenterr.TurnSubstantive {
		t.Fatal("mixed turn must be substantive")
	}
	o := Observation{
		Name: "jv-x", Purpose: "work", ProcessRunning: true,
		TargetID: "T454", MissionOpen: true,
		Phase: "idle", SubstantiveTurn: true,
	}
	cond, _, _ := ClassifyObservation(o)
	if cond != ConditionSatisfied {
		t.Fatalf("mixed turn must satisfy, got %s", cond)
	}
}

func TestT454RefusalOnlySuppressesRungsAndKeepsIncidentOpen(t *testing.T) {
	l := NewLadder()
	// Climb once so an incident exists.
	acts, _ := l.Reconcile(ladderT0.Add(RepressureAfter), idle("jv-x"))
	if len(fired(acts)) != 1 {
		t.Fatalf("setup: want repressure, got %+v", acts)
	}
	// Refusal-only hold: past every threshold, still no further rungs, not closed.
	set := []Gap{{
		Agent: "jv-x", Mission: "T454", Since: ladderT0,
		RefusalOnly: true,
	}}
	acts, closed := l.Reconcile(ladderT0.Add(HumanAlertAfter+time.Hour), set)
	if len(acts) != 0 {
		t.Fatalf("refusal-only must suppress rungs, got %+v", acts)
	}
	if len(closed) != 0 {
		t.Fatalf("refusal-only must not close the incident, got %+v", closed)
	}
	if !l.Tracked("jv-x") {
		t.Fatal("incident must stay open under refusal-only")
	}
}

func TestT454SubstantiveTurnClosesIncidentWithAgentWorkNotice(t *testing.T) {
	l := NewLadder()
	l.Reconcile(ladderT0.Add(RepressureAfter), idle("jv-x"))
	l.Reconcile(ladderT0.Add(HumanAlertAfter), idle("jv-x"))

	at := ladderT0.Add(HumanAlertAfter + time.Minute)
	_, closed := l.Reconcile(at, []Gap{{
		Agent: "jv-x", Mission: "T454", Since: ladderT0, Satisfied: true,
		Cause: ClosedBySatisfaction,
	}})
	if len(closed) != 1 {
		t.Fatalf("want 1 closed incident, got %d", len(closed))
	}
	text := RenderPostmortem(closed[0])
	if !strings.Contains(text, "returned to working") {
		t.Fatalf("agent-work close missing returned-to-working:\n%s", text)
	}
	if strings.Contains(text, "provider began accepting") {
		t.Fatalf("agent-work close must not claim provider resume:\n%s", text)
	}
}

func TestT454ProviderResumeCloseNotice(t *testing.T) {
	l := NewLadder()
	l.Reconcile(ladderT0.Add(HumanAlertAfter), idle("jv-x"))
	at := ladderT0.Add(HumanAlertAfter + time.Minute)
	_, closed := l.Reconcile(at, []Gap{{
		Agent: "jv-x", Mission: "T454", Since: ladderT0, Satisfied: true,
		Cause: ClosedByProviderResume,
	}})
	if len(closed) != 1 || closed[0].Cause != ClosedByProviderResume {
		t.Fatalf("want provider-resume close, got %+v", closed)
	}
	text := RenderPostmortem(closed[0])
	if !strings.Contains(text, "provider began accepting calls again") {
		t.Fatalf("provider-resume notice missing:\n%s", text)
	}
	if strings.Contains(text, "returned to working on its open mission") {
		t.Fatalf("provider-resume must not claim agent resumed work:\n%s", text)
	}
}

// Over-broadness: a ladder that never closes still fails — ordinary resume
// must close unchanged (🎯T454 clause 4).
func TestT454OrdinaryResumeStillCloses(t *testing.T) {
	l := NewLadder()
	l.Reconcile(ladderT0.Add(RepressureAfter), idle("jv-x"))
	acts, closed := l.Reconcile(ladderT0.Add(RepressureAfter+time.Minute), []Gap{{
		Agent: "jv-x", Mission: "T317", Since: ladderT0, Satisfied: true,
	}})
	if len(closed) != 1 {
		t.Fatalf("ordinary resume must close, got closed=%d acts=%+v", len(closed), acts)
	}
	if closed[0].Cause != ClosedBySatisfaction {
		t.Fatalf("ordinary resume cause=%v, want satisfaction", closed[0].Cause)
	}
}

func TestT454ProviderResumeObservationReason(t *testing.T) {
	o := Observation{
		Name: "jv-x", Purpose: "work", ProcessRunning: true,
		TargetID: "T454", MissionOpen: true,
		Phase: "working", ProviderResume: true,
	}
	cond, _, why := ClassifyObservation(o)
	if cond != ConditionSatisfied || why != "provider_resumed_service" {
		t.Fatalf("got cond=%s why=%s", cond, why)
	}
	view, ok := (Outcome{Resolution: ResolutionSatisfied, Reason: why, Gap: MissionGap{Agent: "jv-x", Mission: "T454", OpenedAt: ladderT0}}).LadderView()
	if !ok || view.Cause != ClosedByProviderResume {
		t.Fatalf("LadderView cause=%v ok=%v", view.Cause, ok)
	}
}
