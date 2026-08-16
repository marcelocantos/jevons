// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/eventlog"
	"github.com/marcelocantos/jevons/internal/staffops"
)

// 🎯T407 harness oracle: the three fixture states through sampleSentinel
// + RunCycle. Red against the pre-fix tree, which filed stall:frontier /
// po_stall whenever ready leaves sat idle, including when consume was
// paused or the provider had just refused the fleet.

func t407ReadyLedger(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	ledger := `
schema_version: 1
project: test
targets:
  T500:
    name: ordinary ready Build leaf
    status: identified
    acceptance: ["ship hermetic fix"]
  T501:
    name: second ready Build leaf
    status: identified
    acceptance: ["ship other hermetic fix"]
`
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte(ledger), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func t407Now() time.Time {
	return time.Date(2026, 8, 10, 2, 38, 0, 0, time.UTC)
}

func t407PrescribesSpawn(res staffops.CycleResult) bool {
	if res.Primary != staffops.ActionFilePO || len(res.FiledSymptoms) == 0 {
		return false
	}
	return strings.Contains(staffops.FormatPOMission(res), "spawn")
}

func t407HasKind(sigs []staffops.Signal, kind string) bool {
	for _, s := range sigs {
		if s.Kind == kind {
			return true
		}
	}
	return false
}

func TestT407HealthyReadyLeavesPrescribeSpawn(t *testing.T) {
	dir := t407ReadyLedger(t)
	s := New("/tmp", nil, nil)
	res, _ := s.runSentinelCycle(SentinelLoopArgs{
		Server: s, Workdir: dir, DryRun: true,
		Now: func() time.Time { return t407Now() },
	})
	if !t407PrescribesSpawn(res) {
		t.Fatalf("healthy+ready must prescribe spawn; primary=%s wire=\n%s", res.Primary, res.WireText)
	}
	found := false
	for _, d := range res.Decisions {
		if d.Signal.Kind == "frontier_stall" && d.Action == staffops.ActionFilePO {
			found = true
		}
	}
	if !found {
		t.Fatalf("want frontier_stall file+PO; decisions=%+v", res.Decisions)
	}
}

func TestT407AutoSpawnPausedNoSpawnMission(t *testing.T) {
	dir := t407ReadyLedger(t)
	s := New("/tmp", nil, nil)
	s.SetAutoSpawnPaused(true)

	sigs, resources := s.sampleSentinel(SentinelLoopArgs{Server: s, Workdir: dir}, t407Now())
	if t407HasKind(sigs, "frontier_stall") {
		t.Fatalf("paused sample must not emit frontier_stall: %+v", sigs)
	}
	if t407HasKind(sigs, "po_fanout_stall") {
		t.Fatalf("paused sample must not emit po_fanout_stall: %+v", sigs)
	}
	if !t407HasKind(sigs, "fleet_blocked") {
		t.Fatalf("paused sample must report fleet_blocked: %+v", sigs)
	}
	if !strings.Contains(resources.Note, "auto_spawn_paused") {
		t.Fatalf("resource note must name the pause: %q", resources.Note)
	}

	var delivered []string
	s.SetNotify(func(text string) { delivered = append(delivered, text) })
	res, act := s.runSentinelCycle(SentinelLoopArgs{
		Server: s, Workdir: dir, DryRun: false,
		Now: func() time.Time { return t407Now() },
	})
	if t407PrescribesSpawn(res) {
		t.Fatalf("paused cycle prescribed spawn:\n%s", staffops.FormatPOMission(res))
	}
	if act.FiledToPO {
		t.Fatalf("paused cycle delivered a PO mission: %+v", act)
	}
	joined := strings.Join(delivered, "\n")
	if strings.Contains(joined, "spawn Build workers") {
		t.Fatalf("paused cycle delivered a spawn mission:\n%s", joined)
	}
	if res.Primary == staffops.ActionFilePO {
		for _, d := range res.Decisions {
			if d.Action == staffops.ActionFilePO &&
				(d.Signal.Kind == "frontier_stall" || d.Signal.Kind == "po_fanout_stall") {
				t.Fatalf("paused file+PO on stall: %+v", d)
			}
		}
	}
}

func TestT407QuotaBlockedNoSpawnMission(t *testing.T) {
	dir := t407ReadyLedger(t)
	state := t.TempDir()
	path := eventlog.DefaultPath(state)
	j, err := eventlog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ts := t407Now().Format(time.RFC3339Nano)
	for i := 0; i < 3; i++ {
		if err := j.Append(eventlog.Event{
			TS:        ts,
			Source:    "server",
			Level:     "warn",
			Msg:       "provider_failure",
			Component: "provider_failure",
			Decision:  "rate_limit",
			Fields: map[string]any{
				"failure_class": "rate_limit",
				"raw":           "You've hit your monthly spend limit",
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	_ = j.Close()

	s := New("/tmp", nil, nil)
	s.SetEventLogTailer(func(opt eventlog.TailOptions) ([]eventlog.Event, string, error) {
		ev, err := eventlog.Tail(path, opt)
		return ev, path, err
	})

	sigs, resources := s.sampleSentinel(SentinelLoopArgs{
		Server: s, Workdir: dir, StateDir: state,
	}, t407Now())
	if t407HasKind(sigs, "frontier_stall") {
		t.Fatalf("quota-blocked sample must not emit frontier_stall: %+v", sigs)
	}
	if !t407HasKind(sigs, "fleet_blocked") {
		t.Fatalf("quota-blocked sample must report fleet_blocked: %+v", sigs)
	}
	if !strings.Contains(resources.Note, "provider_quota") {
		t.Fatalf("resource note must name the quota wall: %q", resources.Note)
	}

	res, act := s.runSentinelCycle(SentinelLoopArgs{
		Server: s, Workdir: dir, StateDir: state, DryRun: true,
		Now: func() time.Time { return t407Now() },
	})
	if t407PrescribesSpawn(res) {
		t.Fatalf("quota-blocked cycle prescribed spawn:\n%s", staffops.FormatPOMission(res))
	}
	if act.FiledToPO {
		t.Fatalf("dry-run must not file; act=%+v", act)
	}
	if !strings.Contains(res.WireText, "provider_quota") &&
		!strings.Contains(res.WireText, "blocked:provider_quota") {
		t.Fatalf("wire must name the quota wall:\n%s", res.WireText)
	}
}

func TestT407ArgsAutoSpawnPausedMatchesServerField(t *testing.T) {
	dir := t407ReadyLedger(t)
	s := New("/tmp", nil, nil)
	// Args alone, no Server stamp — the loop can pass config without a setter.
	sigs, _ := s.sampleSentinel(SentinelLoopArgs{
		Server: s, Workdir: dir, AutoSpawnPaused: true,
	}, t407Now())
	if t407HasKind(sigs, "frontier_stall") || !t407HasKind(sigs, "fleet_blocked") {
		t.Fatalf("args pause must block: %+v", sigs)
	}
}

func TestEventRowFromEventCarriesFailureClass(t *testing.T) {
	row := eventRowFromEvent(eventlog.Event{
		TS: "2026-08-09T23:48:00Z", Source: "server", Level: "warn",
		Msg: "provider_failure", Component: "provider_failure",
		Decision: "auth", Fields: map[string]any{"failure_class": "auth"},
	})
	if row.FailureClass != "auth" {
		t.Fatalf("failure_class lost: %+v", row)
	}
	fails := staffops.CollectProviderFailures([]staffops.EventRow{row})
	if len(fails) != 1 || fails[0].Class != "auth" {
		t.Fatalf("collect: %+v", fails)
	}
}

func TestT407OverBroadNeverSpawnFailsHealthy(t *testing.T) {
	dir := t407ReadyLedger(t)
	s := New("/tmp", nil, nil)
	res := t407CycleResult(t, s, dir, false)
	if !t407PrescribesSpawn(res) {
		t.Fatal("healthy fixture no longer prescribes spawn — over-broadness control is dead")
	}
	// Mutant: drop every file+PO. The blocked fixtures would still pass.
	mutant := res
	mutant.Primary = staffops.ActionHarnessOK
	mutant.FiledSymptoms = nil
	if t407PrescribesSpawn(mutant) {
		t.Fatal("mutant still looks like spawn")
	}
}

func t407CycleResult(t *testing.T, s *Server, dir string, paused bool) staffops.CycleResult {
	t.Helper()
	s.SetAutoSpawnPaused(paused)
	res, _ := s.runSentinelCycle(SentinelLoopArgs{
		Server: s, Workdir: dir, DryRun: true,
		Now: func() time.Time { return t407Now() },
	})
	return res
}
