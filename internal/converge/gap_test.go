// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package converge

import "testing"

// 🎯T316 acceptance 1: observation ≠ step ≠ satisfaction, stated as a
// decidable classifier. Every row here is a satisfaction-semantics pin.
func TestClassifyObservationSatisfactionSemantics(t *testing.T) {
	base := Observation{Name: "jv-t316", Purpose: "work", ProcessRunning: true, TargetID: "T316", MissionOpen: true}

	for _, tc := range []struct {
		name     string
		mutate   func(o *Observation)
		wantCond Condition
		wantKind GapKind
		wantWhy  string
	}{
		{
			name:     "idle on open mission is a gap",
			mutate:   func(o *Observation) { o.Phase = "idle" },
			wantCond: ConditionGap, wantKind: GapKindIdle, wantWhy: "not_working_mission_open",
		},
		{
			name:     "blocked on open mission is a gap",
			mutate:   func(o *Observation) { o.Phase = "blocked" },
			wantCond: ConditionGap, wantKind: GapKindIdle, wantWhy: "not_working_mission_open",
		},
		{
			name:     "unknown phase on open mission is a gap",
			mutate:   func(o *Observation) { o.Phase = "" },
			wantCond: ConditionGap, wantKind: GapKindIdle, wantWhy: "not_working_mission_open",
		},
		{
			name:     "dead handle with open mission is a gap",
			mutate:   func(o *Observation) { o.Phase = "idle"; o.ProcessRunning = false },
			wantCond: ConditionGap, wantKind: GapKindDeadHandle, wantWhy: "process_gone_mission_open",
		},
		{
			name:     "claims done without ledger closure is still a gap",
			mutate:   func(o *Observation) { o.Phase = "idle"; o.ClaimsDone = true },
			wantCond: ConditionGap, wantKind: GapKindUnverifiedDone, wantWhy: "claims_done_without_evidence",
		},
		{
			name:     "working on the open mission is satisfaction",
			mutate:   func(o *Observation) { o.Phase = "working" },
			wantCond: ConditionSatisfied, wantWhy: "working_on_open_mission",
		},
		{
			name:     "ledger closure is satisfaction even while idle",
			mutate:   func(o *Observation) { o.Phase = "idle"; o.MissionClosed = true },
			wantCond: ConditionSatisfied, wantWhy: "mission_closed",
		},
		{
			name:     "reap is satisfaction of the agent gap",
			mutate:   func(o *Observation) { o.Phase = "idle"; o.Reaped = true },
			wantCond: ConditionSatisfied, wantWhy: "agent_reaped",
		},
		{
			name:     "deliberate stop is withdrawn, not satisfied",
			mutate:   func(o *Observation) { o.Phase = "idle"; o.DeliberateStop = true },
			wantCond: ConditionOutOfScope, wantWhy: "deliberate_stop",
		},
		{
			name:     "design gated is withdrawn, not satisfied",
			mutate:   func(o *Observation) { o.Phase = "idle"; o.DesignGated = true },
			wantCond: ConditionOutOfScope, wantWhy: "design_gated",
		},
		{
			name:     "no open mission means nothing is wanted",
			mutate:   func(o *Observation) { o.Phase = "idle"; o.MissionOpen = false },
			wantCond: ConditionOutOfScope, wantWhy: "no_open_mission",
		},
		{
			name:     "aside is not a work mission",
			mutate:   func(o *Observation) { o.Phase = "idle"; o.Purpose = "aside" },
			wantCond: ConditionOutOfScope, wantWhy: "not_work_purpose",
		},
		{
			name:     "overseer is not a work mission",
			mutate:   func(o *Observation) { o.Phase = "idle"; o.Purpose = "overseer" },
			wantCond: ConditionOutOfScope, wantWhy: "not_work_purpose",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := base
			tc.mutate(&o)
			cond, kind, why := ClassifyObservation(o)
			if cond != tc.wantCond {
				t.Fatalf("condition = %q, want %q (reason %q)", cond, tc.wantCond, why)
			}
			if kind != tc.wantKind {
				t.Fatalf("kind = %q, want %q", kind, tc.wantKind)
			}
			if why != tc.wantWhy {
				t.Fatalf("reason = %q, want %q", why, tc.wantWhy)
			}
		})
	}
}

// An unbound implementer (no target id) whose mission the caller still holds
// open stays a gap: the owner's "idle with mission open" case does not require
// a bound ledger id.
func TestClassifyUnboundImplementerIdleIsGap(t *testing.T) {
	cond, kind, _ := ClassifyObservation(Observation{
		Name: "jv-loose", ProcessRunning: true, Phase: "idle", MissionOpen: true,
	})
	if cond != ConditionGap || kind != GapKindIdle {
		t.Fatalf("unbound idle implementer: got %q/%q, want gap/idle", cond, kind)
	}
}

// Classification is a function of the observation alone — step history is not
// an input, so no amount of nudging can argue a gap away.
func TestClassificationIgnoresStepHistory(t *testing.T) {
	o := Observation{Name: "jv-t316", ProcessRunning: true, Phase: "idle", TargetID: "T316", MissionOpen: true}
	first, _, _ := ClassifyObservation(o)
	for i := 0; i < 50; i++ {
		if got, _, _ := ClassifyObservation(o); got != first {
			t.Fatalf("classification drifted after %d calls: %q → %q", i, first, got)
		}
	}
	if first != ConditionGap {
		t.Fatalf("expected a standing gap, got %q", first)
	}
}
