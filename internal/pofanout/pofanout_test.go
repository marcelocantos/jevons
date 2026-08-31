// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package pofanout

import (
	"fmt"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/poproactive"
)

// idlePO is the shape the fault is about: alive, not mid-turn, past grace.
func idlePO() POObs {
	return POObs{Name: "jevons-po", Alive: true, Phase: "idle", GraceElapsed: true}
}

// readyLeaf is an ordinary Build leaf with nothing gating it and nobody on it.
func readyLeaf(id string) poproactive.LeafObs {
	return poproactive.LeafObs{ID: id, Name: "ordinary build leaf " + id}
}

// 🎯T380 acceptance (4), fault arm: an idle PO on ready, unengaged, non-gated
// leaves is a fault, not ordinary idle.
func TestIdlePOOnReadyLeavesIsFault(t *testing.T) {
	t.Parallel()
	res := Classify(idlePO(), []poproactive.LeafObs{readyLeaf("T500"), readyLeaf("T501")})
	if res.Verdict != VerdictStalled {
		t.Fatalf("verdict=%s want %s (detail=%s)", res.Verdict, VerdictStalled, res.Detail)
	}
	if !res.Fault() {
		t.Fatal("idle PO on ready leaves must surface as a fault")
	}
	if len(res.ReadyIDs) != 2 {
		t.Fatalf("ready ids=%v want the two stranded leaves", res.ReadyIDs)
	}
	if !strings.Contains(res.Detail, "T500") || !strings.Contains(res.Detail, "T501") {
		t.Fatalf("detail must cite the stranded leaves: %q", res.Detail)
	}
}

// 🎯T380 acceptance (2)+(4), sleep arm: an all-gated frontier is 🎯T325.1
// sleep. The gate classes come from the ledger via poproactive, never from the
// agent's name.
func TestIdlePOOnGatedFrontierIsNotFault(t *testing.T) {
	t.Parallel()
	leaves := []poproactive.LeafObs{
		{ID: "T112", Name: "design-gated hub", Tags: []string{"design-gated"}},
		{ID: "T254", Name: "parked factory", Tags: []string{"parked-for-design"}},
		{ID: "T262.4", Name: "second user", Tags: []string{"needs-owner"}},
	}
	res := Classify(idlePO(), leaves)
	if res.Verdict != VerdictSleepOK {
		t.Fatalf("verdict=%s want %s (detail=%s)", res.Verdict, VerdictSleepOK, res.Detail)
	}
	if res.Fault() {
		t.Fatal("all-gated frontier is legitimate sleep, not a fan-out fault")
	}
	if res.Reason != "only_gated_or_engaged" {
		t.Fatalf("reason=%q want the ledger's own sleep reason", res.Reason)
	}
}

// 🎯T380 acceptance (4), engaged arm: every leaf already has an implementer, so
// there is nothing left for the PO to spawn.
func TestIdlePOOnFullyEngagedFrontierIsNotFault(t *testing.T) {
	t.Parallel()
	leaves := []poproactive.LeafObs{
		{ID: "T500", Name: "leaf one", AlreadyEngaged: true},
		{ID: "T501", Name: "leaf two", AlreadyEngaged: true},
	}
	res := Classify(idlePO(), leaves)
	if res.Verdict != VerdictSleepOK || res.Fault() {
		t.Fatalf("fully engaged frontier: verdict=%s fault=%v want sleep_ok", res.Verdict, res.Fault())
	}
}

// 🎯T380 acceptance (2): an empty frontier is sleep for the same reason.
func TestIdlePOOnEmptyFrontierIsNotFault(t *testing.T) {
	t.Parallel()
	res := Classify(idlePO(), nil)
	if res.Verdict != VerdictSleepOK || res.Reason != "empty_frontier" {
		t.Fatalf("empty frontier: verdict=%s reason=%q want sleep_ok/empty_frontier", res.Verdict, res.Reason)
	}
}

// 🎯T380 acceptance (3): the observation that distinguishes a PO which answered
// a spawn order from one that ended its turn idle having spawned nothing. Both
// are "idle" in agent_list; only the turn record tells them apart.
func TestTurnEndedZeroChildrenIsDistinguishableFromAnswered(t *testing.T) {
	t.Parallel()
	leaves := []poproactive.LeafObs{readyLeaf("T500")}

	silent := idlePO()
	silent.TurnEnded = true
	silent.NewChildrenThisTurn = 0
	got := Classify(silent, leaves)
	if got.Verdict != VerdictTurnNoFanout {
		t.Fatalf("silent turn: verdict=%s want %s", got.Verdict, VerdictTurnNoFanout)
	}
	if !got.Fault() {
		t.Fatal("a turn that ended with zero new children on ready leaves is a fault")
	}
	if !strings.Contains(got.Detail, "new_children=0") {
		t.Fatalf("detail must carry the turn evidence: %q", got.Detail)
	}

	answered := idlePO()
	answered.TurnEnded = true
	answered.NewChildrenThisTurn = 3
	answered.LiveWorkChildren = 3
	if got := Classify(answered, leaves); got.Verdict != VerdictAnswered || got.Fault() {
		t.Fatalf("answered turn: verdict=%s fault=%v want answered/false", got.Verdict, got.Fault())
	}
}

// A PO mid-turn is not yet decidable, and a dead one is the dead_agent class.
func TestWorkingAndAbsentAreNotFanoutFaults(t *testing.T) {
	t.Parallel()
	leaves := []poproactive.LeafObs{readyLeaf("T500")}

	working := idlePO()
	working.Phase = "working"
	if got := Classify(working, leaves); got.Verdict != VerdictWorking || got.Fault() {
		t.Fatalf("mid-turn: verdict=%s fault=%v", got.Verdict, got.Fault())
	}

	dead := idlePO()
	dead.Alive = false
	if got := Classify(dead, leaves); got.Verdict != VerdictAbsent || got.Fault() {
		t.Fatalf("dead PO: verdict=%s fault=%v", got.Verdict, got.Fault())
	}
}

// Between two turns a PO is briefly idle; the grace bound keeps that quiet.
func TestWithinGraceIsNotYetFault(t *testing.T) {
	t.Parallel()
	po := idlePO()
	po.GraceElapsed = false
	res := Classify(po, []poproactive.LeafObs{readyLeaf("T500")})
	if res.Verdict != VerdictWithinGrace || res.Fault() {
		t.Fatalf("within grace: verdict=%s fault=%v", res.Verdict, res.Fault())
	}
}

// An unknown phase reads as idle, matching the idle-nudge sweep's reading of a
// tracker that has seen no events for an agent — otherwise a PO the tracker
// never observed would be silently exempt from the whole check.
func TestUnknownPhaseCountsAsIdle(t *testing.T) {
	t.Parallel()
	po := idlePO()
	po.Phase = ""
	if res := Classify(po, []poproactive.LeafObs{readyLeaf("T500")}); !res.Fault() {
		t.Fatalf("unknown phase must not exempt a PO: verdict=%s", res.Verdict)
	}
}

// ClassifyAll + Faults are the caller's whole interface: one frontier, many
// POs, only the faulty ones surfaced.
func TestClassifyAllSurfacesOnlyFaults(t *testing.T) {
	t.Parallel()
	leaves := []poproactive.LeafObs{readyLeaf("T500")}
	busy := POObs{Name: "other-po", Alive: true, Phase: "working"}
	stalled := idlePO()

	results := ClassifyAll([]POObs{busy, stalled, {Name: "  "}}, leaves)
	if len(results) != 2 {
		t.Fatalf("classified %d POs, want 2 (blank name skipped)", len(results))
	}
	faults := Faults(results)
	if len(faults) != 1 || faults[0].Name != "jevons-po" {
		t.Fatalf("faults=%+v want only jevons-po", faults)
	}
}

// The detail line stays compact when a frontier strands more leaves than it is
// worth naming on the wire.
func TestDetailCapsReadyIDs(t *testing.T) {
	t.Parallel()
	var leaves []poproactive.LeafObs
	for _, id := range []string{"T1", "T2", "T3", "T4", "T5", "T6", "T7", "T8", "T9", "T10"} {
		leaves = append(leaves, readyLeaf(id))
	}
	res := Classify(idlePO(), leaves)
	if !strings.Contains(res.Detail, "ready=10") {
		t.Fatalf("detail must report the true count: %q", res.Detail)
	}
	if !strings.Contains(res.Detail, "+2 more") {
		t.Fatalf("detail must elide beyond the cap: %q", res.Detail)
	}
	if strings.Contains(res.Detail, "T9") {
		t.Fatalf("detail cited past the cap: %q", res.Detail)
	}
}

// 🎯T586 tape one: the 2026-08-29 shape. Thirty-three genuinely ready leaves,
// a fleet already at six live seats so the governor refuses a new pane, and a
// provider that has hit its spend limit. The PO was correctly declining to
// mint, and the sentinel must not call that stalled.
func TestCapacityAndSpendBlockedPOIsNotStalled(t *testing.T) {
	t.Parallel()
	var leaves []poproactive.LeafObs
	for i := range 33 {
		leaves = append(leaves, readyLeaf(fmt.Sprintf("T%d", 500+i)))
	}

	po := idlePO()
	po.LiveWorkChildren = 4
	po.SpawnRefused = true
	po.SpawnRefusedReason = "seat_count"
	po.SpawnRefusedDetail = "seat-count runaway; refusing a new worker pane (🎯T566.2)"
	po.ProviderBlocked = true
	po.ProviderReason = "spend_limit"
	po.ProviderDetail = "fable weekly allowance exhausted"

	res := Classify(po, leaves)
	if res.Fault() {
		t.Fatalf("blocked PO surfaced as a fault: verdict=%s detail=%s", res.Verdict, res.Detail)
	}
	if res.Verdict != VerdictProviderExhausted {
		t.Fatalf("verdict=%s want %s", res.Verdict, VerdictProviderExhausted)
	}
	if res.Blocker != BlockerProviderExhausted {
		t.Fatalf("blocker=%q want %q", res.Blocker, BlockerProviderExhausted)
	}
	if res.Reason == "idle_on_ready_leaves" {
		t.Fatal("reason must name the true blocker, not idle_on_ready_leaves")
	}
	if !strings.Contains(res.Detail, "blocker=provider_exhausted") {
		t.Fatalf("detail must carry the blocker: %q", res.Detail)
	}
	if !strings.Contains(res.Detail, "ready=33") {
		t.Fatalf("detail must still report the engageable count: %q", res.Detail)
	}

	// The same PO with the provider healthy is still host-refused, and the
	// verdict then names the host rather than the provider.
	po.ProviderBlocked, po.ProviderReason, po.ProviderDetail = false, "", ""
	res = Classify(po, leaves)
	if res.Fault() || res.Verdict != VerdictCapacityRefused {
		t.Fatalf("host-refused PO: verdict=%s fault=%v want capacity_refused/false", res.Verdict, res.Fault())
	}
	if res.Blocker != BlockerCapacityRefused || res.Reason != "seat_count" {
		t.Fatalf("blocker=%q reason=%q want capacity_refused/seat_count", res.Blocker, res.Reason)
	}
	if !strings.Contains(res.Detail, "🎯T566.2") {
		t.Fatalf("detail must carry the governor's own evidence: %q", res.Detail)
	}
}

// 🎯T586 tape one, turn arm: a turn whose whole purpose was fleet repair ended
// with zero new children while the host refused panes. That is not
// turn_no_fanout either — the earlier 08:11 / 08:31 false alarms.
func TestBlockedTurnWithZeroChildrenIsNotTurnNoFanout(t *testing.T) {
	t.Parallel()
	po := idlePO()
	po.TurnEnded = true
	po.NewChildrenThisTurn = 0
	po.SpawnRefused = true
	po.SpawnRefusedReason = "memory_grind"

	res := Classify(po, []poproactive.LeafObs{readyLeaf("T500")})
	if res.Fault() {
		t.Fatalf("repair turn under a host refusal is not a fan-out fault: verdict=%s", res.Verdict)
	}
	if res.Verdict != VerdictCapacityRefused || res.Reason != "memory_grind" {
		t.Fatalf("verdict=%s reason=%q want capacity_refused/memory_grind", res.Verdict, res.Reason)
	}
}

// 🎯T586 tape two, the control: a genuinely idle PO with admitting capacity, a
// willing provider and one ungated leaf is still stalled. An over-broad fix
// that silences the sentinel fails here.
func TestIdlePOWithCapacityAndOneUngatedLeafIsStillStalled(t *testing.T) {
	t.Parallel()
	res := Classify(idlePO(), []poproactive.LeafObs{readyLeaf("T500")})
	if !res.Fault() || res.Verdict != VerdictStalled {
		t.Fatalf("verdict=%s fault=%v want stalled/true", res.Verdict, res.Fault())
	}
	if res.Reason != "idle_on_ready_leaves" {
		t.Fatalf("reason=%q want idle_on_ready_leaves", res.Reason)
	}
	if res.Blocker != "" {
		t.Fatalf("blocker=%q want empty — nothing was in this PO's way", res.Blocker)
	}
}

// 🎯T586: an all-gated frontier names all_gated as its blocker, so the
// overseer reads one vocabulary across the three legitimate reasons.
func TestGatedFrontierNamesAllGatedBlocker(t *testing.T) {
	t.Parallel()
	leaves := []poproactive.LeafObs{
		{ID: "T112", Name: "design-gated hub", Tags: []string{"design-gated"}},
	}
	res := Classify(idlePO(), leaves)
	if res.Blocker != BlockerAllGated {
		t.Fatalf("blocker=%q want %q", res.Blocker, BlockerAllGated)
	}
	if res.Fault() {
		t.Fatal("all-gated frontier is sleep, not a fault")
	}
	// An empty frontier has no leaves to gate, so it names no blocker.
	if got := Classify(idlePO(), nil); got.Blocker != "" {
		t.Fatalf("empty frontier blocker=%q want empty", got.Blocker)
	}
}
