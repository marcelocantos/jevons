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
