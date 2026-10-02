// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package staffops

import (
	"testing"

	"github.com/marcelocantos/jevons/internal/fleetintent"
)

// 🎯T984: a seat running against its intent is a fault even though its intent
// is parked — the one stood-down signal the intent check must not swallow —
// and a seat the broker runs unknown to this daemon is a mechanical fault.
func TestT984SeatsAgainstIntentAndDivergedAreRepairedAfterGrace(t *testing.T) {
	in := ObserveInput{
		FleetIntent: fleetintent.Working,
		AgentIntent: map[string]fleetintent.State{"parked": fleetintent.Parked},
		Agents: []AgentObs{
			{Name: "parked", AgainstIntent: "intent is parked but the broker runs it unowned", GraceElapsed: true},
			{Name: "lost", Diverged: "the broker runs this seat", GraceElapsed: true},
			{Name: "fresh", AgainstIntent: "intent is parked", GraceElapsed: false},
			{Name: "quiet", DeliberateStop: true},
		},
	}
	got := map[string]Decision{}
	for _, sig := range BuildSignals(in) {
		got[sig.Symptom] = Classify(sig)
	}
	for sym, want := range map[string]Action{
		"intent:parked": ActionRepair,
		"diverge:lost":  ActionRepair,
		"intent:fresh":  ActionIgnore,
		"stop:quiet":    ActionIgnore,
	} {
		d, ok := got[sym]
		if !ok {
			t.Fatalf("no signal %q in %v", sym, got)
		}
		if d.Action != want {
			t.Errorf("%s: %s (%s), want %s", sym, d.Action, d.Reason, want)
		}
	}
}
