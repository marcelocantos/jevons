// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package staffops

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/fleetintent"
)

// 🎯T410 hermetic oracle. Three fixture fleet states plus an over-broadness
// mutation: a sentinel that never prescribes repair must fail the stalled
// case, or the genuine-stall alarm is silently dead.

func t410Now() time.Time {
	return time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)
}

func t410Cycle(in ObserveInput) CycleResult {
	return RunCycle(CycleArgs{
		Signals:  BuildSignals(in),
		Sentinel: true,
		Now:      t410Now(),
	})
}

func t410AgentBase(name string) AgentObs {
	return AgentObs{
		Name:         name,
		Phase:        "idle",
		Alive:        true,
		OpenMission:  true,
		IdleResidue:  true,
		GraceElapsed: true,
		BoundTarget:  "T410",
	}
}

func t410StalledInput() ObserveInput {
	a := t410AgentBase("jv-stalled")
	// No finish / owner-ask evidence — genuinely stalled.
	return ObserveInput{
		OverseerAlive:    true,
		OverseerAttached: true,
		Agents:           []AgentObs{a},
	}
}

func t410FinishedInput() ObserveInput {
	a := t410AgentBase("jv-finished")
	a.ReportLooksFinished = true
	a.HasBoundCommits = true
	a.TargetLedgerStatus = "identified"
	return ObserveInput{
		OverseerAlive:    true,
		OverseerAttached: true,
		Agents:           []AgentObs{a},
	}
}

func t410BlockedInput() ObserveInput {
	a := t410AgentBase("jv-blocked")
	a.OwnerAskPresent = true
	a.IntentBlockedOwner = true
	return ObserveInput{
		OverseerAlive:    true,
		OverseerAttached: true,
		Agents:           []AgentObs{a},
		AgentIntent: map[string]fleetintent.State{
			"jv-blocked": fleetintent.BlockedOwner,
		},
	}
}

func t410PrescribesRepair(res CycleResult) bool {
	if res.Primary != ActionRepair {
		return false
	}
	for _, d := range res.Decisions {
		if d.Action == ActionRepair && d.Signal.Kind == "fleet_idle_residue" {
			return true
		}
	}
	return false
}

func t410PrescribesCloseTarget(res CycleResult) bool {
	if len(res.FiledSymptoms) == 0 {
		return false
	}
	mission := FormatPOMission(res)
	if !strings.Contains(mission, "close") && !strings.Contains(mission, "achieve") {
		return false
	}
	if strings.Contains(mission, "spawn Build") {
		return false
	}
	for _, d := range res.Decisions {
		if d.Signal.Kind == "finished_awaiting_gate" && d.Action == ActionFilePO {
			return true
		}
	}
	return false
}

func t410SurfacesOwnerAsk(res CycleResult) bool {
	if t410PrescribesRepair(res) {
		return false
	}
	for _, d := range res.Decisions {
		if d.Signal.Kind != "blocked_on_owner" {
			continue
		}
		if d.Action == ActionRepair {
			return false
		}
		if strings.Contains(d.Reason, "surface ask") || strings.Contains(res.WireText, "surface") {
			return true
		}
	}
	return strings.Contains(res.WireText, "blocked-on-owner") ||
		strings.Contains(res.WireText, "blocked on owner")
}

func t410WantStalled(res CycleResult) []string {
	var errs []string
	if !t410PrescribesRepair(res) {
		errs = append(errs, "stalled must prescribe repair; primary="+string(res.Primary))
	}
	if t410PrescribesCloseTarget(res) {
		errs = append(errs, "stalled must not prescribe close-target")
	}
	if t410SurfacesOwnerAsk(res) && !t410PrescribesRepair(res) {
		errs = append(errs, "stalled must not be read as blocked-on-owner")
	}
	return errs
}

func t410WantFinished(res CycleResult) []string {
	var errs []string
	if t410PrescribesRepair(res) {
		errs = append(errs, "finished-awaiting-gate prescribed repair:\n"+res.WireText)
	}
	if !t410PrescribesCloseTarget(res) {
		errs = append(errs, "finished-awaiting-gate must prescribe close-target; primary="+string(res.Primary)+"\n"+FormatPOMission(res))
	}
	if strings.Contains(res.WireText, "rehydrate/interrupt/nudge") &&
		!strings.Contains(res.WireText, "do not rehydrate") {
		errs = append(errs, "finished wire still nudges the worker:\n"+res.WireText)
	}
	return errs
}

func t410WantBlocked(res CycleResult) []string {
	var errs []string
	if t410PrescribesRepair(res) {
		errs = append(errs, "blocked-on-owner prescribed repair:\n"+res.WireText)
	}
	if t410PrescribesCloseTarget(res) {
		errs = append(errs, "blocked-on-owner prescribed close-target:\n"+FormatPOMission(res))
	}
	if !t410SurfacesOwnerAsk(res) {
		errs = append(errs, "blocked-on-owner must surface ask to owner:\n"+res.WireText)
	}
	return errs
}

func TestClassifyIdleResiduePriority(t *testing.T) {
	none := ClassifyIdleResidue(IdleResidueEvidence{})
	if none.Class != IdleResidueNone {
		t.Fatalf("empty: %q", none.Class)
	}
	stalled := ClassifyIdleResidue(IdleResidueEvidence{
		IdleResidue: true, OpenMission: true, BoundTarget: "T410",
	})
	if stalled.Class != IdleResidueStalled {
		t.Fatalf("stalled: %q", stalled.Class)
	}
	finished := ClassifyIdleResidue(IdleResidueEvidence{
		IdleResidue: true, OpenMission: true, BoundTarget: "T410",
		ReportLooksFinished: true, TargetLedgerStatus: "identified",
	})
	if finished.Class != IdleResidueFinishedAwaitingGate {
		t.Fatalf("finished: %q detail=%q", finished.Class, finished.Detail)
	}
	byCommits := ClassifyIdleResidue(IdleResidueEvidence{
		IdleResidue: true, OpenMission: true, BoundTarget: "T410",
		HasBoundCommits: true, TargetLedgerStatus: "converging",
	})
	if byCommits.Class != IdleResidueFinishedAwaitingGate {
		t.Fatalf("commits: %q", byCommits.Class)
	}
	blocked := ClassifyIdleResidue(IdleResidueEvidence{
		IdleResidue: true, OpenMission: true, BoundTarget: "T410",
		ReportLooksFinished: true, OwnerAskPresent: true,
	})
	if blocked.Class != IdleResidueBlockedOnOwner {
		t.Fatalf("owner ask outranks finish: %q", blocked.Class)
	}
	intent := ClassifyIdleResidue(IdleResidueEvidence{
		IdleResidue: true, OpenMission: true, IntentBlockedOwner: true,
	})
	if intent.Class != IdleResidueBlockedOnOwner {
		t.Fatalf("intent: %q", intent.Class)
	}
	// Achieved ledger is not awaiting gate.
	closed := ClassifyIdleResidue(IdleResidueEvidence{
		IdleResidue: true, OpenMission: true,
		HasBoundCommits: true, TargetLedgerStatus: "achieved",
	})
	if closed.Class != IdleResidueStalled {
		t.Fatalf("achieved target: %q want stalled residual", closed.Class)
	}
}

func TestT410StalledPrescribesRepair(t *testing.T) {
	res := t410Cycle(t410StalledInput())
	for _, err := range t410WantStalled(res) {
		t.Error(err)
	}
	if !strings.Contains(res.WireText, "Act: bounded control-plane repair") {
		t.Fatalf("stalled wire missing repair act:\n%s", res.WireText)
	}
}

func TestT410FinishedAwaitingGateCloseTargetNoNudge(t *testing.T) {
	res := t410Cycle(t410FinishedInput())
	for _, err := range t410WantFinished(res) {
		t.Error(err)
	}
	found := false
	for _, d := range res.Decisions {
		if d.Signal.Kind == "finished_awaiting_gate" {
			found = true
			if d.Action != ActionFilePO {
				t.Fatalf("action=%s want file+PO", d.Action)
			}
			if strings.Contains(d.Reason, "nudge") && !strings.Contains(d.Reason, "no worker nudge") {
				t.Fatalf("reason=%q", d.Reason)
			}
		}
		if d.Signal.Kind == "fleet_idle_residue" {
			t.Fatalf("finished must not emit fleet_idle_residue: %+v", d)
		}
	}
	if !found {
		t.Fatalf("missing finished_awaiting_gate: %+v", res.Decisions)
	}
}

func TestT410BlockedOnOwnerSurfacesAskNoRepair(t *testing.T) {
	res := t410Cycle(t410BlockedInput())
	for _, err := range t410WantBlocked(res) {
		t.Error(err)
	}
	for _, d := range res.Decisions {
		if d.Signal.Kind == "fleet_idle_residue" {
			t.Fatalf("blocked must not emit fleet_idle_residue: %+v", d)
		}
		if d.Action == ActionRepair {
			t.Fatalf("blocked prescribed repair: %+v", d)
		}
	}
}

func TestClassifyFinishedAwaitingGateAndBlocked(t *testing.T) {
	fin := Classify(Signal{
		Kind: "finished_awaiting_gate", Symptom: "finished:jv-x",
		Severity: "medium", Detail: "target=T410",
	})
	if fin.Action != ActionFilePO {
		t.Fatalf("finished action=%s", fin.Action)
	}
	if !strings.Contains(fin.Reason, "close target") {
		t.Fatalf("reason=%q", fin.Reason)
	}
	blk := Classify(Signal{
		Kind: "blocked_on_owner", Symptom: "owner_block:jv-y",
		Severity: "medium", Intent: fleetintent.BlockedOwner,
		Detail: "needs keypress",
	})
	if blk.Action != ActionHarnessOK {
		t.Fatalf("blocked action=%s", blk.Action)
	}
	if !strings.Contains(blk.Reason, "no repair") {
		t.Fatalf("reason=%q", blk.Reason)
	}
}

func TestT410OracleDetectsOverBroadness(t *testing.T) {
	// Control: the real path still repairs a genuine stall.
	stalled := t410Cycle(t410StalledInput())
	if !t410PrescribesRepair(stalled) {
		t.Fatal("oracle would pass a never-repair sentinel — stalled fixture is red")
	}
	// Mutant: a sentinel that never prescribes repair. Finished and blocked
	// checks still pass; the genuine-stall alarm is dead.
	mutant := stalled
	mutant.Primary = ActionHarnessOK
	mutant.RepairSymptoms = nil
	mutant.Decisions = append([]Decision(nil), stalled.Decisions...)
	for i := range mutant.Decisions {
		if mutant.Decisions[i].Action == ActionRepair {
			mutant.Decisions[i].Action = ActionIgnore
			mutant.Decisions[i].Reason = "mutant: never repair"
		}
	}
	if t410PrescribesRepair(mutant) {
		t.Fatal("mutant still looks like a repair instruction")
	}
	if errs := t410WantStalled(mutant); len(errs) == 0 {
		t.Fatal("oracle passed a sentinel that never prescribes repair")
	}
	// Finished and blocked fixtures must still demand their own prescriptions
	// so the mutant cannot hide behind "everything is harness-ok".
	if errs := t410WantFinished(t410Cycle(t410FinishedInput())); len(errs) != 0 {
		t.Fatalf("finished fixture broken: %v", errs)
	}
	if errs := t410WantBlocked(t410Cycle(t410BlockedInput())); len(errs) != 0 {
		t.Fatalf("blocked fixture broken: %v", errs)
	}
}

func TestT410PhaseIdleAloneIsNotFinishedOrBlocked(t *testing.T) {
	// Pre-fix shape: IdleResidue+OpenMission+Grace with no evidence → stalled
	// repair only. Evidence fields empty must not invent finish/block.
	a := t410AgentBase("jv-phase-only")
	res := t410Cycle(ObserveInput{
		OverseerAlive: true,
		Agents:        []AgentObs{a},
	})
	if !t410PrescribesRepair(res) {
		t.Fatalf("phase-only idle must still repair: primary=%s\n%s", res.Primary, res.WireText)
	}
	for _, d := range res.Decisions {
		if d.Signal.Kind == "finished_awaiting_gate" || d.Signal.Kind == "blocked_on_owner" {
			t.Fatalf("phase alone invented %s: %+v", d.Signal.Kind, d)
		}
	}
}
