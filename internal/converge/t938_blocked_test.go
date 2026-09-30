// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package converge

import (
	"testing"
	"time"
)

// 🎯T938: a seat whose stored finish-report declares it blocked on the owner
// is out of scope for the impatience ladder; the same idle shape without the
// declaration is still a gap.
func TestT938BlockedOnOwnerIsOutOfScope(t *testing.T) {
	o := Observation{Name: "w", Phase: "idle", ProcessRunning: true, TargetID: "T1", MissionOpen: true, BlockedOnOwner: true}
	cond, kind, reason := ClassifyObservation(o)
	if cond != ConditionOutOfScope || kind != "" || reason != "blocked_on_owner" {
		t.Fatalf("got %s/%s/%s", cond, kind, reason)
	}
	o.BlockedOnOwner = false
	if cond, _, _ := ClassifyObservation(o); cond != ConditionGap {
		t.Fatalf("control: without the blocker the idle is a gap, got %s", cond)
	}
}

// An open incident is withdrawn, not satisfied, when the seat declares a
// blocker: nothing was resolved, the parent holds it now.
func TestT938BlockedWithdrawsOpenGap(t *testing.T) {
	s := NewSet()
	now := time.Unix(1_700_000_000, 0)
	o := Observation{Name: "w", Phase: "idle", ProcessRunning: true, TargetID: "T1", MissionOpen: true}
	if out := s.Reconcile(o, now); out.Resolution != ResolutionOpened {
		t.Fatalf("idle open mission: resolution %s, want opened", out.Resolution)
	}
	o.BlockedOnOwner = true
	out := s.Reconcile(o, now.Add(time.Minute))
	if out.Resolution != ResolutionWithdrawn || out.Reason != "blocked_on_owner" {
		t.Fatalf("blocked: resolution %s reason %s, want withdrawn/blocked_on_owner", out.Resolution, out.Reason)
	}
	if s.Len() != 0 {
		t.Fatalf("set still holds %d gaps", s.Len())
	}
}
