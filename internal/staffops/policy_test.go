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

func TestClassifyDeliberateStop(t *testing.T) {
	d := Classify(Signal{
		Kind: "deliberate_stop", Symptom: "stop:w",
		DeliberateStop: true, Severity: "high",
	})
	if d.Action != ActionIgnore {
		t.Fatalf("got %s want ignore", d.Action)
	}
	if !strings.Contains(d.Reason, "deliberate") {
		t.Fatalf("reason=%q", d.Reason)
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

func TestActionBudgetMaxPerHour(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	b := &ActionBudget{MaxPerHour: 2}
	if !b.Allow(now) {
		t.Fatal("empty budget should allow")
	}
	b.Record(now)
	b.Record(now.Add(time.Minute))
	if b.Allow(now.Add(2 * time.Minute)) {
		t.Fatal("at max should deny")
	}
	// After window slides past first two.
	if !b.Allow(now.Add(61 * time.Minute)) {
		t.Fatal("after hour should allow again")
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

func TestRunCycleSentinelWire(t *testing.T) {
	res := RunCycle(CycleArgs{
		Sentinel: true,
		Now:      time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC),
	})
	if !strings.Contains(res.WireText, "T219") {
		t.Fatalf("sentinel wire:\n%s", res.WireText)
	}
	if !strings.Contains(res.WireText, "no product implement") {
		t.Fatalf("missing no-implement:\n%s", res.WireText)
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
		Cooldown:  cd,
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
		Signals:  []Signal{sig},
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

func TestRunCycleRateLimit(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	budget := &ActionBudget{MaxPerHour: 1}
	sig := Signal{
		Kind: "frontier_stall", Symptom: "stall:A",
		Severity: "high", Mechanical: false,
	}
	res1 := RunCycle(CycleArgs{
		Signals: []Signal{sig},
		Budget:  budget,
		Now:     now,
	})
	if res1.Primary != ActionFilePO {
		t.Fatalf("first=%s", res1.Primary)
	}
	// Second distinct symptom still rate-limited.
	sig2 := Signal{
		Kind: "frontier_stall", Symptom: "stall:B",
		Severity: "high", Mechanical: false,
	}
	res2 := RunCycle(CycleArgs{
		Signals: []Signal{sig2},
		Budget:  budget,
		Now:     now.Add(time.Minute),
	})
	if res2.Primary != ActionIgnore {
		t.Fatalf("rate-limited primary=%s want ignore", res2.Primary)
	}
	if !strings.Contains(res2.WireText, "max actions/hour") {
		t.Fatalf("wire:\n%s", res2.WireText)
	}
}

func TestRunCycleDryRunDoesNotMarkCooldown(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	cd := &Cooldown{Duration: time.Hour}
	budget := &ActionBudget{MaxPerHour: 1}
	sig := Signal{
		Kind: "gap", Symptom: "gap:x", Severity: "medium",
	}
	res := RunCycle(CycleArgs{
		Signals: []Signal{sig},
		Cooldown: cd,
		Budget:  budget,
		Now:     now,
		DryRun:  true,
	})
	if res.Primary != ActionFilePO {
		t.Fatalf("primary=%s", res.Primary)
	}
	if cd.ShouldSuppress("gap:x", now.Add(time.Minute)) {
		t.Fatal("dry-run must not mark cooldown")
	}
	if budget.Count(now) != 0 {
		t.Fatal("dry-run must not record budget")
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
	if len(res.RepairSymptoms) != 1 {
		t.Fatalf("repair symptoms=%v", res.RepairSymptoms)
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
	for _, a := range want {
		d := Decision{Action: a, Reason: "t", Signal: Signal{Symptom: "s", Kind: "k"}}
		res := CycleResult{Primary: a, Decisions: []Decision{d}}
		wire := FormatReport(res, false)
		if !strings.Contains(wire, string(a)) {
			t.Errorf("wire missing %s:\n%s", a, wire)
		}
	}
}

func TestFormatPOMission(t *testing.T) {
	res := CycleResult{
		Primary:       ActionFilePO,
		FiledSymptoms: []string{"stall:frontier"},
		Decisions: []Decision{{
			Action: ActionFilePO,
			Reason: "non-mechanical residual — file+PO",
			Signal: Signal{
				Kind: "frontier_stall", Symptom: "stall:frontier",
				Detail: "ready leaves=3",
			},
		}},
	}
	m := FormatPOMission(res)
	for _, want := range []string{"jevons-po", "T219", "spawn", "no Ship", "stall:frontier"} {
		if !strings.Contains(m, want) {
			t.Errorf("mission missing %q:\n%s", want, m)
		}
	}
}

func TestBuildSignalsObserveSurfaces(t *testing.T) {
	sigs := BuildSignals(ObserveInput{
		OverseerAlive:     false,
		OverseerGraceDone: true,
		Agents: []AgentObs{
			{Name: "w1", DeadHandle: true, GraceElapsed: true},
			{Name: "w2", DeliberateStop: true},
			{Name: "w3", OpenMission: true, IdleResidue: true, GraceElapsed: false},
		},
		Events: []EventObs{
			{Kind: "busy_storm", Symptom: "event:busy_storm", Count: 6},
		},
		FrontierDepth:   2,
		FrontierStalled: true,
		CostAlerts:      []CostObs{{Kind: "global-rate", Severity: "medium"}},
	})
	kinds := map[string]bool{}
	for _, s := range sigs {
		kinds[s.Kind] = true
		if s.Kind == "deliberate_stop" && !s.DeliberateStop {
			t.Fatal("deliberate_stop signal must set DeliberateStop")
		}
	}
	for _, k := range []string{
		"overseer_down", "dead_agent", "deliberate_stop",
		"fleet_idle_residue", "busy_storm", "frontier_stall", "cost_alert",
	} {
		if !kinds[k] {
			t.Errorf("missing kind %s in %+v", k, sigs)
		}
	}
}

// 🎯T346: stall only when unattended-ready depth ≥ 1 (not raw graph depth).
func TestFrontierStallObsReadyOnly(t *testing.T) {
	// All gated / parked hubs → depth 0, not stalled (capacity-zero sleep OK).
	depth, stalled, detail := FrontierStallObs(0, 0)
	if depth != 0 || stalled {
		t.Fatalf("gated-only: depth=%d stalled=%v detail=%q", depth, stalled, detail)
	}
	// Raw depth must not be faked via FrontierStalled alone without ready count.
	sigs := BuildSignals(ObserveInput{
		OverseerAlive:   true,
		FrontierDepth:   0,
		FrontierStalled: false,
		// Simulate mis-set: stalled true but depth 0 must not emit.
	})
	for _, s := range sigs {
		if s.Kind == "frontier_stall" {
			t.Fatalf("depth=0 must not emit frontier_stall: %+v", s)
		}
	}
	sigs = BuildSignals(ObserveInput{
		OverseerAlive:   true,
		FrontierDepth:   0,
		FrontierStalled: true, // inconsistent — BuildSignals still requires depth>0
	})
	for _, s := range sigs {
		if s.Kind == "frontier_stall" {
			t.Fatalf("stalled+depth0 must not emit frontier_stall: %+v", s)
		}
	}

	// One Build leaf, no engaged workers → stall depth 1.
	depth, stalled, detail = FrontierStallObs(1, 0)
	if depth != 1 || !stalled {
		t.Fatalf("one ready: depth=%d stalled=%v detail=%q", depth, stalled, detail)
	}
	if !strings.Contains(detail, "unattended-ready leaves=1") {
		t.Fatalf("detail=%q", detail)
	}
	// Engaged work cancels stall (fleet already working).
	depth, stalled, _ = FrontierStallObs(3, 1)
	if depth != 3 || stalled {
		t.Fatalf("engaged: depth=%d stalled=%v", depth, stalled)
	}

	depth, stalled, detail = FrontierStallObsWithIDs([]string{"T500"}, 0)
	if depth != 1 || !stalled {
		t.Fatalf("with ids: depth=%d stalled=%v", depth, stalled)
	}
	if !strings.Contains(detail, "T500") || !strings.Contains(detail, "unattended-ready") {
		t.Fatalf("detail=%q", detail)
	}

	// Pipeline: gated-only observe → no file+PO from frontier.
	depth, stalled, detail = FrontierStallObs(0, 0)
	sigs = BuildSignals(ObserveInput{
		OverseerAlive:   true,
		FrontierDepth:   depth,
		FrontierStalled: stalled,
		FrontierDetail:  detail,
	})
	for _, s := range sigs {
		if s.Kind == "frontier_stall" {
			t.Fatalf("all-gated harness must not file+PO stall: %+v", s)
		}
	}
	res := RunCycle(CycleArgs{
		Signals:  sigs,
		Sentinel: true,
		Now:      time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC),
	})
	if res.Primary == ActionFilePO {
		t.Fatalf("all-gated primary must not be file+PO: %+v", res)
	}

	// One Build leaf → stall → file+PO.
	depth, stalled, detail = FrontierStallObsWithIDs([]string{"T500"}, 0)
	sigs = BuildSignals(ObserveInput{
		OverseerAlive:   true,
		FrontierDepth:   depth,
		FrontierStalled: stalled,
		FrontierDetail:  detail,
	})
	var stallSig *Signal
	for i := range sigs {
		if sigs[i].Kind == "frontier_stall" {
			stallSig = &sigs[i]
			break
		}
	}
	if stallSig == nil {
		t.Fatal("expected frontier_stall for one Build leaf")
	}
	if !strings.Contains(stallSig.Detail, "T500") {
		t.Fatalf("stall detail must cite Build leaf: %q", stallSig.Detail)
	}
	res = RunCycle(CycleArgs{
		Signals:  sigs,
		Sentinel: true,
		Now:      time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC),
	})
	if res.Primary != ActionFilePO {
		t.Fatalf("one Build leaf primary=%s want file+PO decisions=%+v", res.Primary, res.Decisions)
	}
}

func TestClusterEventAnomalies(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	rows := []EventRow{
		{Msg: "notify_queue not_running for agent x", TS: now},
		{Msg: "notify_queue not_running again", TS: now},
		{Msg: "notify_queue not_running thrice", TS: now},
		{Msg: "restart thrash detected", TS: now},
		{Msg: "busy storm on fleet", TS: now},
		{Level: "error", Component: "server", Msg: "panic recover", TS: now},
		{Level: "error", Component: "server", Msg: "panic recover 2", TS: now},
		{Msg: "old", TS: now.Add(-time.Hour)}, // outside window
	}
	obs := ClusterEventAnomalies(rows, now, 15*time.Minute)
	if len(obs) < 3 {
		t.Fatalf("obs=%+v", obs)
	}
	found := map[string]int{}
	for _, o := range obs {
		found[o.Kind] = o.Count
	}
	if found["notify_queue"] < 3 {
		t.Fatalf("notify_queue count=%d", found["notify_queue"])
	}
	if found["restart_thrash"] < 1 {
		t.Fatal("missing restart_thrash")
	}
	if found["busy_storm"] < 1 {
		t.Fatal("missing busy_storm")
	}
}

func TestObserveThenClassifyPipeline(t *testing.T) {
	// End-to-end pure oracle: signal→harness-ok|repair|file+PO|ignore.
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	sigs := BuildSignals(ObserveInput{
		OverseerAlive: true,
		Agents: []AgentObs{
			{Name: "dead-w", DeadHandle: true, GraceElapsed: true},
			{Name: "ok-stop", DeliberateStop: true},
		},
		FrontierDepth:   1,
		FrontierStalled: true,
	})
	res := RunCycle(CycleArgs{
		Signals:  sigs,
		Sentinel: true,
		Now:      now,
		Budget:   &ActionBudget{MaxPerHour: 10},
	})
	// Primary should be file+PO (frontier) or repair (dead) — file ranks higher.
	if res.Primary != ActionFilePO && res.Primary != ActionRepair {
		t.Fatalf("primary=%s decisions=%+v", res.Primary, res.Decisions)
	}
	// Deliberate stop must classify ignore.
	for _, d := range res.Decisions {
		if d.Signal.DeliberateStop && d.Action != ActionIgnore {
			t.Fatalf("deliberate stop not ignored: %+v", d)
		}
	}
}

// 🎯T380: a PO fan-out fault reaches the wire as a non-mechanical residual the
// overseer must decide on. Only faults are ever passed in — a legitimately
// sleeping PO produces no observation, so a correctly-idle PO can never appear
// in the report at all.
func TestBuildSignalsPOFanoutFault(t *testing.T) {
	sigs := BuildSignals(ObserveInput{
		OverseerAlive: true, OverseerAttached: true,
		POFanout: []POFanoutObs{
			{Name: "jevons-po", Verdict: "stalled", Reason: "idle_on_ready_leaves",
				ReadyCount: 2, Detail: "po=jevons-po ready=2 [T500,T501]"},
			{Name: "squz-po", Verdict: POFanoutTurnNoFanout, Reason: "turn_ended_zero_new_children",
				ReadyCount: 1},
			{Name: "  ", Verdict: "stalled"},
		},
	})

	var stalled, silent *Signal
	for i := range sigs {
		if sigs[i].Kind != "po_fanout_stall" {
			continue
		}
		switch sigs[i].Symptom {
		case "po_stall:jevons-po":
			stalled = &sigs[i]
		case "po_stall:squz-po":
			silent = &sigs[i]
		default:
			t.Fatalf("unexpected fan-out symptom %q (blank names must be dropped)", sigs[i].Symptom)
		}
	}
	if stalled == nil || silent == nil {
		t.Fatalf("want both fan-out faults on the wire; got %+v", sigs)
	}
	if stalled.Mechanical {
		t.Fatal("fan-out silence is not a class T204/T207/T85 already owns")
	}
	if stalled.Severity != "high" {
		t.Fatalf("stalled severity=%q want high", stalled.Severity)
	}
	if silent.Severity != "critical" {
		t.Fatalf("turn-no-fanout severity=%q want critical — the PO was awake", silent.Severity)
	}
	if stalled.Detail != "po=jevons-po ready=2 [T500,T501]" {
		t.Fatalf("supplied detail must survive verbatim: %q", stalled.Detail)
	}
	if !strings.Contains(silent.Detail, "turn_ended_zero_new_children") ||
		!strings.Contains(silent.Detail, "ready=1") {
		t.Fatalf("missing detail must be synthesised from the verdict: %q", silent.Detail)
	}

	// Both must classify file+PO: nothing mechanical repairs a PO's judgement.
	for _, sig := range []*Signal{stalled, silent} {
		if got := Classify(*sig); got.Action != ActionFilePO {
			t.Fatalf("%s → %s want %s", sig.Symptom, got.Action, ActionFilePO)
		}
	}
}
