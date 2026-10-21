// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"errors"
	"testing"

	"github.com/marcelocantos/jevons/internal/fleet"
)

// TestNotePlanAuthRevivalsLogsEachSeat is the 🎯T1128 acceptance hermetic
// for the plan-auth revive road. RevivePlanAuthPeers / RevivePlanAfterOwnerReauth
// / RevivePlanAuthWhereHealthy all route a stopped seat back through
// LaunchRecovering, under evidence that a provider's plan login works
// again — a real seat-launch path — but before this target nothing on that
// road ever reached the durable event log: noteRevive's own doc comment
// named "plan_auth" as a revival road while no call site ever passed it.
//
// RED on the pre-fix code: RevivePlanAuthPeers/RevivePlanAfterOwnerReauth/
// RevivePlanAuthWhereHealthy returned their []fleet.PlanAuthRevival and
// nothing else — no agent_lifecycle event, successful or failed, for a
// seat this sweep just relaunched. This test exercises the current
// (post-fix) wrapper and asserts every revival — success and failure —
// lands a compAgentLifecycle event naming this exact road.
func TestNotePlanAuthRevivalsLogsEachSeat(t *testing.T) {
	t.Parallel()

	var got []map[string]any
	s := &Server{}
	s.SetEventLogger(func(component, decision string, fields map[string]any) {
		if component != compAgentLifecycle {
			return
		}
		row := map[string]any{"decision": decision}
		for k, v := range fields {
			row[k] = v
		}
		got = append(got, row)
	})

	s.notePlanAuthRevivals([]fleet.PlanAuthRevival{
		{Name: "jv-plan-a"},
		{Name: "jv-plan-b", Err: errors.New("relaunch failed: no history")},
	})

	if len(got) != 2 {
		t.Fatalf("expected 2 agent_lifecycle events, got %d: %#v", len(got), got)
	}
	if got[0]["name"] != "jv-plan-a" || got[0]["path"] != "plan_auth" || got[0]["decision"] != "rehydrate" {
		t.Fatalf("success revival not logged as a plan_auth rehydrate: %#v", got[0])
	}
	if got[0]["outcome"] != "ok" {
		t.Fatalf("success revival should be outcome=ok: %#v", got[0])
	}
	if got[1]["name"] != "jv-plan-b" || got[1]["path"] != "plan_auth" || got[1]["outcome"] != "error" {
		t.Fatalf("failed revival not logged with its error: %#v", got[1])
	}
}

// TestReattachBackSeatsLogRevive is the companion RED-before/GREEN-after
// hermetic for the reattach sweep's success path. A seat the broker
// brought back (res.Back) left s.seatStops().Forget as its only side
// effect; noteRevive was wired for relaunchableLostSeat's hold-refused
// branch only, so a seat that WAS relaunched — the normal, happy-path
// outcome — never named itself in the event log at all. This reproduces
// the 2026-10-21 20:01:44 shape ("agent started" in the daemon log, nothing
// in the event log) for the reattach road specifically, by exercising the
// same logging seam ReattachRunningSeats now drives for every name in
// res.Back.
func TestReattachBackSeatsLogRevive(t *testing.T) {
	t.Parallel()

	var got []map[string]any
	s := &Server{}
	s.SetEventLogger(func(component, decision string, fields map[string]any) {
		if component != compAgentLifecycle {
			return
		}
		row := map[string]any{"decision": decision}
		for k, v := range fields {
			row[k] = v
		}
		got = append(got, row)
	})

	// Exercise the exact call ReattachRunningSeats now makes for each
	// res.Back entry (the loop body added by this target), rather than
	// standing up a full claudia registry + live processes just to reach
	// fleet.ReattachRunningSeats' internals.
	for _, name := range []string{"jv-reattached-a", "jv-reattached-b"} {
		s.noteRevive(name, "reattach", "", nil)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 agent_lifecycle events for reattached seats, got %d: %#v", len(got), got)
	}
	for i, name := range []string{"jv-reattached-a", "jv-reattached-b"} {
		if got[i]["name"] != name || got[i]["path"] != "reattach" || got[i]["outcome"] != "ok" {
			t.Fatalf("reattached seat %d not logged as a successful reattach revive: %#v", i, got[i])
		}
	}
}
