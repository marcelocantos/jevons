// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package staffops

import (
	"strings"
	"testing"
	"time"
)

// 🎯T407 hermetic oracle. Three fixture states plus an over-broadness
// mutation: a sentinel that never prescribes spawn must fail the healthy
// case, or the genuine-gap alarm is silently dead.

func t407Now() time.Time {
	return time.Date(2026, 8, 10, 2, 38, 0, 0, time.UTC)
}

func t407HealthyInput() ObserveInput {
	return ObserveInput{
		OverseerAlive:   true,
		OverseerAttached: true,
		FrontierDepth:   3,
		FrontierStalled: true,
		FrontierDetail:  "unattended-ready leaves=3 engaged_workers=0 ready=[T500,T501,T502]",
		POFanout: []POFanoutObs{{
			Name: "jevons-po", Verdict: "stalled", Reason: "idle_on_ready_leaves",
			ReadyCount: 3, Detail: "po=jevons-po ready=3 [T500,T501,T502]",
		}},
	}
}

func t407Cycle(in ObserveInput) CycleResult {
	return RunCycle(CycleArgs{
		Signals:  BuildSignals(in),
		Sentinel: true,
		Now:      t407Now(),
	})
}

// t407PrescribesSpawn is the load-bearing check: file+PO plus a mission
// that tells the PO to spawn. A blocked report that never files is not
// a spawn instruction even if the word "spawn" appears in a reason.
func t407PrescribesSpawn(res CycleResult) bool {
	if res.Primary != ActionFilePO {
		return false
	}
	if len(res.FiledSymptoms) == 0 {
		return false
	}
	return strings.Contains(FormatPOMission(res), "spawn")
}

func t407BlockCause(res CycleResult) string {
	for _, d := range res.Decisions {
		if d.Signal.Kind == "fleet_blocked" {
			return strings.TrimPrefix(d.Signal.Symptom, "blocked:")
		}
	}
	return ""
}

func t407WantSpawn(res CycleResult) []string {
	var errs []string
	if !t407PrescribesSpawn(res) {
		errs = append(errs, "healthy ready leaves must prescribe spawn; primary="+string(res.Primary))
	}
	if t407BlockCause(res) != "" {
		errs = append(errs, "healthy fleet must not report fleet_blocked="+t407BlockCause(res))
	}
	return errs
}

func t407WantBlocked(res CycleResult, wantCause string) []string {
	var errs []string
	if t407PrescribesSpawn(res) {
		errs = append(errs, "blocked fleet prescribed spawn:\n"+FormatPOMission(res))
	}
	if res.Primary == ActionFilePO {
		for _, d := range res.Decisions {
			if d.Action == ActionFilePO && (d.Signal.Kind == "frontier_stall" || d.Signal.Kind == "po_fanout_stall") {
				errs = append(errs, "blocked fleet filed "+d.Signal.Kind+" "+d.Signal.Symptom)
			}
		}
	}
	got := t407BlockCause(res)
	if got != wantCause {
		errs = append(errs, "block cause="+got+" want "+wantCause)
	}
	if !strings.Contains(res.WireText, wantCause) && !strings.Contains(res.WireText, "blocked:"+wantCause) {
		errs = append(errs, "wire does not name the blocking cause:\n"+res.WireText)
	}
	if strings.Contains(res.WireText, "Act: deliver mission to jevons-po") {
		errs = append(errs, "blocked wire still delivers a PO mission:\n"+res.WireText)
	}
	return errs
}

func TestClassifyFleetRunPauseWins(t *testing.T) {
	v := ClassifyFleetRun(FleetRunEvidence{
		AutoSpawnPaused: true,
		ProviderFailures: []ProviderFailureObs{{
			Class: "rate_limit", Count: 3, Detail: "spend limit",
		}},
	})
	if v.Runnable || v.Cause != BlockAutoSpawnPaused {
		t.Fatalf("pause must win: %+v", v)
	}
	if v.Detail != "frontier_consume.disabled" {
		t.Fatalf("detail=%q", v.Detail)
	}
}

func TestClassifyFleetRunQuotaAndAuth(t *testing.T) {
	quota := ClassifyFleetRun(FleetRunEvidence{
		ProviderFailures: []ProviderFailureObs{{
			Class: "rate_limit", Count: 2, Detail: "429",
		}},
	})
	if quota.Runnable || quota.Cause != BlockProviderQuota {
		t.Fatalf("quota: %+v", quota)
	}
	auth := ClassifyFleetRun(FleetRunEvidence{
		ProviderFailures: []ProviderFailureObs{
			{Class: "auth", Count: 1, Detail: "401"},
			{Class: "rate_limit", Count: 4, Detail: "429"},
		},
	})
	if auth.Runnable || auth.Cause != BlockProviderAuth {
		t.Fatalf("auth outranks quota: %+v", auth)
	}
	ok := ClassifyFleetRun(FleetRunEvidence{})
	if !ok.Runnable || ok.Cause != "" {
		t.Fatalf("empty evidence must be runnable: %+v", ok)
	}
	// backend_unavailable is not a wall.
	blip := ClassifyFleetRun(FleetRunEvidence{
		ProviderFailures: []ProviderFailureObs{{
			Class: "backend_unavailable", Count: 5,
		}},
	})
	if !blip.Runnable {
		t.Fatalf("transient backend must not block: %+v", blip)
	}
}

func TestCollectProviderFailuresIgnoresOtherClasses(t *testing.T) {
	got := CollectProviderFailures([]EventRow{
		{Component: "provider_failure", FailureClass: "rate_limit", Msg: "quota"},
		{Component: "provider_failure", FailureClass: "auth", Msg: "key"},
		{Component: "provider_failure", FailureClass: "client_bug", Msg: "wire"},
		{Component: "butler", FailureClass: "rate_limit", Msg: "provider_failure also"},
		{Component: "server", Level: "error", Msg: "panic"},
	})
	if len(got) != 2 {
		t.Fatalf("got=%+v", got)
	}
	if got[0].Class != "auth" || got[0].Count != 1 {
		t.Fatalf("auth first: %+v", got[0])
	}
	if got[1].Class != "rate_limit" || got[1].Count != 2 {
		t.Fatalf("quota clustered: %+v", got[1])
	}
}

func TestT407HealthyReadyLeavesPrescribeSpawn(t *testing.T) {
	res := t407Cycle(t407HealthyInput())
	for _, err := range t407WantSpawn(res) {
		t.Error(err)
	}
	foundStall, foundPO := false, false
	for _, d := range res.Decisions {
		if d.Signal.Kind == "frontier_stall" && d.Action == ActionFilePO {
			foundStall = true
		}
		if d.Signal.Kind == "po_fanout_stall" && d.Action == ActionFilePO {
			foundPO = true
		}
	}
	if !foundStall || !foundPO {
		t.Fatalf("healthy must file both stall and po_stall; decisions=%+v", res.Decisions)
	}
}

func TestT407AutoSpawnPausedReportsBlockNoSpawn(t *testing.T) {
	in := t407HealthyInput()
	in.FleetBlock = ClassifyFleetRun(FleetRunEvidence{AutoSpawnPaused: true}).AsObs()
	res := t407Cycle(in)
	for _, err := range t407WantBlocked(res, BlockAutoSpawnPaused) {
		t.Error(err)
	}
	for _, d := range res.Decisions {
		if d.Signal.Kind == "frontier_stall" || d.Signal.Kind == "po_fanout_stall" {
			t.Fatalf("paused fleet must not emit %s: %+v", d.Signal.Kind, d)
		}
	}
}

func TestT407QuotaBlockedReportsBlockNoSpawn(t *testing.T) {
	in := t407HealthyInput()
	in.FleetBlock = ClassifyFleetRun(FleetRunEvidence{
		ProviderFailures: []ProviderFailureObs{{
			Class: "rate_limit", Count: 3, Detail: "monthly spend limit",
		}},
	}).AsObs()
	res := t407Cycle(in)
	for _, err := range t407WantBlocked(res, BlockProviderQuota) {
		t.Error(err)
	}
}

func TestClassifyFleetBlockedNeverFilesPO(t *testing.T) {
	d := Classify(Signal{
		Kind: "fleet_blocked", Symptom: "blocked:auto_spawn_paused",
		Severity: "critical", Mechanical: false,
		Detail: "frontier_consume.disabled",
	})
	if d.Action != ActionHarnessOK {
		t.Fatalf("action=%s want harness-ok", d.Action)
	}
	if !strings.Contains(d.Reason, "do not spawn") {
		t.Fatalf("reason=%q", d.Reason)
	}
}

func TestT407OracleDetectsOverBroadness(t *testing.T) {
	// Control: the real path still prescribes spawn on a genuine gap.
	healthy := t407Cycle(t407HealthyInput())
	if !t407PrescribesSpawn(healthy) {
		t.Fatal("oracle would pass a never-spawn sentinel — healthy fixture is red")
	}
	// Mutant: a sentinel that never prescribes spawn. Every blocked check
	// still passes; the genuine-gap alarm is dead.
	mutant := healthy
	mutant.Primary = ActionHarnessOK
	mutant.FiledSymptoms = nil
	mutant.Decisions = append([]Decision(nil), healthy.Decisions...)
	for i := range mutant.Decisions {
		if mutant.Decisions[i].Action == ActionFilePO {
			mutant.Decisions[i].Action = ActionIgnore
			mutant.Decisions[i].Reason = "mutant: never spawn"
		}
	}
	if t407PrescribesSpawn(mutant) {
		t.Fatal("mutant still looks like a spawn instruction")
	}
	if errs := t407WantSpawn(mutant); len(errs) == 0 {
		t.Fatal("oracle passed a sentinel that never prescribes spawn")
	}
}

func TestClusterEventAnomaliesSkipsProviderWalls(t *testing.T) {
	now := t407Now()
	obs := ClusterEventAnomalies([]EventRow{
		{Component: "provider_failure", FailureClass: "rate_limit", Level: "error",
			Msg: "provider_failure spend limit", TS: now},
		{Component: "provider_failure", FailureClass: "rate_limit", Level: "error",
			Msg: "provider_failure spend limit 2", TS: now},
	}, now, 15*time.Minute)
	for _, o := range obs {
		if strings.Contains(o.Kind, "daemon_error") || strings.Contains(o.Symptom, "provider_failure") {
			t.Fatalf("provider wall clustered as anomaly: %+v", o)
		}
	}
}
