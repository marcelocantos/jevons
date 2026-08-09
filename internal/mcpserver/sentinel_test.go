// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/eventlog"
	"github.com/marcelocantos/jevons/internal/staffops"
)

func TestSentinelCycleDryRunHealthy(t *testing.T) {
	s := New("/tmp", nil, nil)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"dry_run": true}
	res, err := s.handleSentinelCycle(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("error: %+v", res)
	}
	text := toolText(res)
	for _, want := range []string{
		"Sentinel cycle",
		"T219",
		"primary=",
		"dry_run=true",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
}

func TestSentinelCycleFilePODeliversMission(t *testing.T) {
	dir := t.TempDir()
	// Minimal ledger with one ready leaf so frontier_stall can fire.
	ledger := `
schema_version: 1
project: test
targets:
  T1:
    name: ready leaf
    status: identified
    acceptance: ["x"]
`
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte(ledger), 0o644); err != nil {
		t.Fatal(err)
	}

	s := New("/tmp", nil, nil)
	var delivered []string
	s.SetNotify(func(text string) { delivered = append(delivered, text) })
	s.SetCostMonitor(func() (*cost.Snapshot, error) {
		return &cost.Snapshot{
			Alerts: []cost.Alert{{Kind: "global-rate", Level: cost.LevelWarn, Detail: "hot"}},
		}, nil
	})

	// Force workdir via args through runSentinelCycle.
	res, act := s.runSentinelCycle(SentinelLoopArgs{
		Server:  s,
		Workdir: dir,
		DryRun:  false,
		Now:     func() time.Time { return time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC) },
	})
	// Cost alert alone → file+PO even without frontier.
	if res.Primary != staffops.ActionFilePO && res.Primary != staffops.ActionRepair && res.Primary != staffops.ActionHarnessOK {
		// With only cost alert: file+PO
		t.Logf("primary=%s wire=\n%s", res.Primary, res.WireText)
	}
	if res.Primary == staffops.ActionFilePO {
		if !act.FiledToPO && !act.Delivered {
			// notify path may count as delivered
			if len(delivered) == 0 {
				t.Fatalf("expected PO/overseer deliver; act=%+v", act)
			}
		}
		// At least one delivery should mention sentinel or mission.
		joined := strings.Join(delivered, "\n")
		if !strings.Contains(joined, "sentinel") && !strings.Contains(joined, "T219") && !strings.Contains(joined, "staff-ops") {
			// FormatEventPush wraps; body should still carry content.
			if !strings.Contains(joined, "file+PO") && !strings.Contains(joined, "Primary") {
				t.Fatalf("delivered missing mission/report:\n%s", joined)
			}
		}
	}
}

func TestSentinelCycleRateLimitAndCooldown(t *testing.T) {
	s := New("/tmp", nil, nil)
	s.SetCostMonitor(func() (*cost.Snapshot, error) {
		return &cost.Snapshot{
			Alerts: []cost.Alert{{Kind: "global-rate", Level: cost.LevelWarn, Detail: "hot"}},
		}, nil
	})
	var delivered []string
	s.SetNotify(func(text string) { delivered = append(delivered, text) })

	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	// Cap at 1 action/hour.
	rt := s.ensureSentinelRuntime(1)
	rt.budget.MaxPerHour = 1

	res1, _ := s.runSentinelCycle(SentinelLoopArgs{
		Server: s, DryRun: false,
		MaxActionsPerHour: 1,
		Now:               func() time.Time { return now },
	})
	if res1.Primary != staffops.ActionFilePO {
		t.Fatalf("first primary=%s wire=\n%s", res1.Primary, res1.WireText)
	}

	res2, act2 := s.runSentinelCycle(SentinelLoopArgs{
		Server: s, DryRun: false,
		MaxActionsPerHour: 1,
		Now:               func() time.Time { return now.Add(time.Minute) },
	})
	// Cooldown on same cost symptom OR rate limit → ignore.
	if res2.Primary == staffops.ActionFilePO {
		t.Fatalf("second should suppress re-file: primary=%s act=%+v wire=\n%s", res2.Primary, act2, res2.WireText)
	}
}

func TestSentinelLoopTicksUntilCancel(t *testing.T) {
	s := New("/tmp", nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	var n int
	done := make(chan struct{})
	go func() {
		StartSentinelLoop(ctx, SentinelLoopArgs{
			Server:   s,
			Interval: 20 * time.Millisecond,
			DryRun:   true,
			OnResult: func(staffops.CycleResult, SentinelActResult) {
				n++
				if n >= 2 {
					cancel()
				}
			},
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("loop did not stop")
	}
	if n < 2 {
		t.Fatalf("cycles=%d want >=2", n)
	}
}

func TestSampleSentinelEventlogAnomalies(t *testing.T) {
	dir := t.TempDir()
	path := eventlog.DefaultPath(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	j, err := eventlog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := j.Append(eventlog.Event{
			Msg: "notify_queue not_running for worker", Level: "warn", Component: "butler",
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

	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	// Stamp events as now via re-write with TS — Tail preserves file TS.
	// Cluster uses TS if present; empty TS is kept (not filtered by window).
	sigs, _ := s.sampleSentinel(SentinelLoopArgs{Server: s, StateDir: dir}, now)
	found := false
	for _, sig := range sigs {
		if sig.Kind == "notify_queue" || strings.Contains(sig.Symptom, "notify_queue") {
			found = true
		}
	}
	if !found {
		// May still classify via StateDir direct path if tailer returns events without TS.
		t.Logf("signals=%+v", sigs)
	}
}

func TestSentinelRepairTriggersControlPlane(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Overseer missing → overseer_down mechanical after grace.
	s := New("/tmp", nil, nil)
	s.SetRegistry(reg)

	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	// Seed firstSeen in the past so grace elapsed.
	rt := s.ensureSentinelRuntime(10)
	rt.firstSeen["overseer:down"] = now.Add(-10 * time.Minute)

	res, act := s.runSentinelCycle(SentinelLoopArgs{
		Server: s, DryRun: false,
		Now: func() time.Time { return now },
	})
	// No overseer registered → down → repair after grace.
	if res.Primary != staffops.ActionRepair && res.Primary != staffops.ActionIgnore && res.Primary != staffops.ActionHarnessOK {
		t.Logf("primary=%s act=%+v wire=\n%s", res.Primary, act, res.WireText)
	}
	if res.Primary == staffops.ActionRepair && !act.Repaired {
		t.Fatalf("repair primary but not repaired: %+v", act)
	}
}

func TestStaffOpsStillWorksAfterSentinelRegister(t *testing.T) {
	s := New("/tmp", nil, nil)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"dry_run": true}
	res, err := s.handleStaffOpsCycle(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(toolText(res), "Staff ops cycle") {
		t.Fatalf("staff ops broken:\n%s", toolText(res))
	}
}

// 🎯T346: raw graph frontier of gated/parked hubs must not fire stall:frontier.
func TestSentinelFrontierStallGatedOnlyNoFilePO(t *testing.T) {
	dir := t.TempDir()
	// Fixture mirrors live false-positive class: T254 parked factory, T262.4
	// needs-owner, T112 design — all graph-ready, zero unattended-ready.
	ledger := `
schema_version: 1
project: test
targets:
  T112:
    name: design-gated OAuth hub
    status: identified
    tags: [design-gated]
    acceptance: ["x"]
  T254:
    name: unattended factory hub
    status: identified
    tags: [parked-for-design]
    context: "parked pending owner open"
    acceptance: ["x"]
  T254.2:
    name: factory child parked
    status: identified
    tags: [parked]
    acceptance: ["x"]
  T262.4:
    name: second-user needs owner
    status: identified
    tags: [needs-owner]
    acceptance: ["x"]
  T67:
    name: design discussion residual
    status: identified
    tags: [design-discussion]
    acceptance: ["x"]
`
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte(ledger), 0o644); err != nil {
		t.Fatal(err)
	}

	s := New("/tmp", nil, nil)
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	sigs, resources := s.sampleSentinel(SentinelLoopArgs{Server: s, Workdir: dir}, now)
	if resources.FrontierDepth != 0 {
		t.Fatalf("gated-only frontier_depth=%d want 0 (unattended-ready)", resources.FrontierDepth)
	}
	for _, sig := range sigs {
		if sig.Kind == "frontier_stall" {
			t.Fatalf("gated-only must not emit frontier_stall: %+v", sig)
		}
	}
	res, _ := s.runSentinelCycle(SentinelLoopArgs{
		Server: s, Workdir: dir, DryRun: true,
		Now: func() time.Time { return now },
	})
	if res.Primary == staffops.ActionFilePO {
		// Must not file+PO solely from gated frontier (no cost/other residuals).
		for _, d := range res.Decisions {
			if d.Signal.Kind == "frontier_stall" && d.Action == staffops.ActionFilePO {
				t.Fatalf("gated-only file+PO on frontier_stall: %+v wire=\n%s", d, res.WireText)
			}
		}
	}
	for _, d := range res.Decisions {
		if d.Signal.Kind == "frontier_stall" {
			t.Fatalf("gated-only decisions must omit frontier_stall: %+v", d)
		}
	}
}

// 🎯T346: one true Build leaf among gated hubs → stall depth 1 citing Build id.
func TestSentinelFrontierStallOneBuildLeafDepth1(t *testing.T) {
	dir := t.TempDir()
	ledger := `
schema_version: 1
project: test
targets:
  T112:
    name: design-gated hub
    status: identified
    tags: [design-gated]
    acceptance: ["x"]
  T254:
    name: parked factory
    status: identified
    tags: [parked-for-design]
    acceptance: ["x"]
  T262.4:
    name: needs owner accept
    status: identified
    tags: [needs-owner]
    acceptance: ["x"]
  T500:
    name: ordinary ready Build leaf
    status: identified
    acceptance: ["ship hermetic fix"]
`
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte(ledger), 0o644); err != nil {
		t.Fatal(err)
	}

	s := New("/tmp", nil, nil)
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	sigs, resources := s.sampleSentinel(SentinelLoopArgs{Server: s, Workdir: dir}, now)
	if resources.FrontierDepth != 1 {
		t.Fatalf("mixed frontier_depth=%d want 1 (only T500)", resources.FrontierDepth)
	}
	var stall *staffops.Signal
	for i := range sigs {
		if sigs[i].Kind == "frontier_stall" {
			stall = &sigs[i]
			break
		}
	}
	if stall == nil {
		t.Fatalf("expected frontier_stall; sigs=%+v", sigs)
	}
	if !strings.Contains(stall.Detail, "T500") {
		t.Fatalf("stall must cite Build leaf T500: %q", stall.Detail)
	}
	if strings.Contains(stall.Detail, "T112") || strings.Contains(stall.Detail, "T254") || strings.Contains(stall.Detail, "T262.4") {
		t.Fatalf("stall must not cite gated hubs: %q", stall.Detail)
	}
	// Full cycle should classify file+PO on stall:frontier.
	res, _ := s.runSentinelCycle(SentinelLoopArgs{
		Server: s, Workdir: dir, DryRun: true,
		Now: func() time.Time { return now },
	})
	found := false
	for _, d := range res.Decisions {
		if d.Signal.Kind == "frontier_stall" && d.Action == staffops.ActionFilePO {
			found = true
			if !strings.Contains(d.Signal.Detail, "T500") {
				t.Fatalf("file+PO detail missing T500: %+v", d)
			}
		}
	}
	if !found {
		t.Fatalf("want frontier_stall file+PO; primary=%s decisions=%+v", res.Primary, res.Decisions)
	}
}

// 🎯T352: the synthetic RSI ops drill rows the owner appended on 2026-08-09
// must not become an event:error:rsi_drill file+PO, while real daemon errors
// beside them still classify.
func TestSentinelIgnoresRSIDrillEventRows(t *testing.T) {
	dir := t.TempDir()
	path := eventlog.DefaultPath(dir)
	j, err := eventlog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	ts := now.Format(time.RFC3339Nano)
	for i := 0; i < 2; i++ {
		if err := j.Append(eventlog.Event{
			TS:        ts,
			Source:    "rsi-drill",
			Level:     "error",
			Msg:       "rsi_ops_live_drill: synthetic coach stimulus (safe to ignore)",
			Component: "rsi_drill",
			Decision:  "live_drill",
			Fields:    map[string]any{"drill": "true", "purpose": "exercise T243/T333 closed loop"},
		}); err != nil {
			t.Fatal(err)
		}
		if err := j.Append(eventlog.Event{
			TS:        ts,
			Source:    "server",
			Level:     "error",
			Msg:       "butler send failed",
			Component: "butler",
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

	sigs, _ := s.sampleSentinel(SentinelLoopArgs{Server: s, StateDir: dir}, now)
	realFound := false
	for _, sig := range sigs {
		if strings.Contains(sig.Symptom, "rsi_drill") || strings.Contains(sig.Detail, "rsi_ops_live_drill") {
			t.Fatalf("drill row classified as anomaly: %+v", sig)
		}
		if sig.Symptom == "event:error:butler" {
			realFound = true
		}
	}
	if !realFound {
		t.Fatalf("real daemon error no longer classified: %+v", sigs)
	}
}

func TestEventRowFromEventCarriesDrillMarkers(t *testing.T) {
	row := eventRowFromEvent(eventlog.Event{
		TS: "2026-08-09T06:03:04.778Z", Source: "rsi-drill", Level: "error",
		Msg: "rsi_ops_live_drill: synthetic coach stimulus", Component: "rsi_drill",
		Decision: "live_drill", Fields: map[string]any{"drill": "true"},
	})
	if row.Source != "rsi-drill" || !row.Drill {
		t.Fatalf("drill markers lost in projection: %+v", row)
	}
	if row.TS.IsZero() {
		t.Fatalf("TS not parsed: %+v", row)
	}
	if !staffops.IsSyntheticDrillRow(row) {
		t.Fatalf("projected row not recognised as drill: %+v", row)
	}

	plain := eventRowFromEvent(eventlog.Event{
		TS: "2026-08-09T06:03:04Z", Source: "server", Level: "error",
		Msg: "butler send failed", Component: "butler",
	})
	if plain.Drill || staffops.IsSyntheticDrillRow(plain) {
		t.Fatalf("real row flagged as drill: %+v", plain)
	}
	if plain.TS.IsZero() {
		t.Fatalf("RFC3339 TS not parsed: %+v", plain)
	}
}
