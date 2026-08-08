// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package staffops

import (
	"strings"
	"testing"
	"time"
)

func TestClassifyMechanicalGraceAndRepair(t *testing.T) {
	within := Classify(Signal{
		Kind: "dead_agent", Symptom: "dead:worker-a",
		Mechanical: true, GraceElapsed: false,
	})
	if within.Action != ActionIgnore {
		t.Fatalf("within grace: got %s want ignore", within.Action)
	}

	after := Classify(Signal{
		Kind: "dead_agent", Symptom: "dead:worker-a",
		Mechanical: true, GraceElapsed: true,
	})
	if after.Action != ActionRepair {
		t.Fatalf("after grace: got %s want repair", after.Action)
	}

	recovered := Classify(Signal{
		Kind: "dead_agent", Symptom: "dead:worker-a",
		Mechanical: true, HarnessActed: true, GraceElapsed: true,
	})
	if recovered.Action != ActionHarnessOK {
		t.Fatalf("harness acted: got %s want harness-ok", recovered.Action)
	}
}

func TestClassifyNonMechanicalFilePO(t *testing.T) {
	d := Classify(Signal{
		Kind: "frontier_stall", Symptom: "stall:T99",
		Severity: "high", Mechanical: false,
	})
	if d.Action != ActionFilePO {
		t.Fatalf("got %s want file+PO", d.Action)
	}

	low := Classify(Signal{
		Kind: "noise", Symptom: "noise:x",
		Severity: "low", Mechanical: false,
	})
	if low.Action != ActionIgnore {
		t.Fatalf("low: got %s want ignore", low.Action)
	}
}

func TestCooldownSuppressesRefile(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	cd := &Cooldown{Duration: time.Hour}
	cd.MarkFiled("stall:T99", now)

	if !cd.ShouldSuppress("stall:T99", now.Add(30*time.Minute)) {
		t.Fatal("expected suppress inside cooldown")
	}
	if cd.ShouldSuppress("stall:T99", now.Add(2*time.Hour)) {
		t.Fatal("expected no suppress after cooldown")
	}
	if cd.ShouldSuppress("other", now.Add(30*time.Minute)) {
		t.Fatal("other symptom must not suppress")
	}
}

func TestRunCycleHealthyEmpty(t *testing.T) {
	res := RunCycle(CycleArgs{
		Resources: ResourceSnapshot{SessionCount: 2, RunningAgents: 1, FrontierDepth: 3},
		Now:       time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC),
	})
	if res.Primary != ActionHarnessOK {
		t.Fatalf("primary=%s", res.Primary)
	}
	if !strings.Contains(res.WireText, "no signals") {
		t.Fatalf("wire missing healthy path:\n%s", res.WireText)
	}
	if !strings.Contains(res.WireText, "sessions=2") {
		t.Fatalf("wire missing snapshot:\n%s", res.WireText)
	}
	if !strings.Contains(res.WireText, "T325.4") {
		t.Fatalf("wire missing target marker:\n%s", res.WireText)
	}
}

func TestRunCycleFilePOCooldownAndPrimary(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	cd := &Cooldown{Duration: time.Hour}
	sig := Signal{
		Kind: "frontier_stall", Symptom: "stall:T99",
		Severity: "high", Mechanical: false,
	}
	res1 := RunCycle(CycleArgs{
		Signals:   []Signal{sig},
		Resources: ResourceSnapshot{IdlePOCount: 1},
		Cooldown:   cd,
		Now:       now,
	})
	if res1.Primary != ActionFilePO {
		t.Fatalf("first primary=%s", res1.Primary)
	}
	if len(res1.FiledSymptoms) != 1 || res1.FiledSymptoms[0] != "stall:T99" {
		t.Fatalf("filed=%v", res1.FiledSymptoms)
	}
	if !strings.Contains(res1.WireText, "file+PO") {
		t.Fatalf("wire:\n%s", res1.WireText)
	}

	// Same symptom inside cooldown → ignore; primary drops.
	res2 := RunCycle(CycleArgs{
		Signals: []Signal{sig},
		Cooldown: cd,
		Now:      now.Add(10 * time.Minute),
	})
	if res2.Primary != ActionIgnore {
		t.Fatalf("second primary=%s want ignore", res2.Primary)
	}
	if len(res2.FiledSymptoms) != 0 {
		t.Fatalf("second filed=%v", res2.FiledSymptoms)
	}
	if !strings.Contains(res2.WireText, "cooldown") {
		t.Fatalf("second wire missing cooldown:\n%s", res2.WireText)
	}
}

func TestRunCycleDryRunDoesNotMarkCooldown(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	cd := &Cooldown{Duration: time.Hour}
	sig := Signal{
		Kind: "gap", Symptom: "gap:x", Severity: "medium",
	}
	res := RunCycle(CycleArgs{
		Signals: []Signal{sig},
		Cooldown: cd,
		Now:      now,
		DryRun:   true,
	})
	if res.Primary != ActionFilePO {
		t.Fatalf("primary=%s", res.Primary)
	}
	if cd.ShouldSuppress("gap:x", now.Add(time.Minute)) {
		t.Fatal("dry-run must not mark cooldown")
	}
}

func TestRunCycleRepairBeatsIgnore(t *testing.T) {
	res := RunCycle(CycleArgs{
		Signals: []Signal{
			{Kind: "noise", Symptom: "n", Severity: "low"},
			{Kind: "dead_agent", Symptom: "d:w", Mechanical: true, GraceElapsed: true},
		},
		Now: time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC),
	})
	if res.Primary != ActionRepair {
		t.Fatalf("primary=%s want repair", res.Primary)
	}
}

func TestAllActionsPresentInWireVocabulary(t *testing.T) {
	// Oracle: policy vocabulary matches T219/T325.4 acceptance.
	want := []Action{ActionHarnessOK, ActionRepair, ActionFilePO, ActionIgnore}
	for _, a := range want {
		if a == "" {
			t.Fatal("empty action")
		}
	}
	// Round-trip string form used on wire.
	for _, a := range want {
		d := Decision{Action: a, Reason: "t", Signal: Signal{Symptom: "s", Kind: "k"}}
		res := CycleResult{Primary: a, Decisions: []Decision{d}}
		wire := FormatReport(res)
		if !strings.Contains(wire, string(a)) {
			t.Errorf("wire missing %s:\n%s", a, wire)
		}
	}
}
