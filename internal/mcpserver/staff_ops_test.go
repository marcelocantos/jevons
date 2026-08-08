// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/staffops"
)

func TestStaffOpsCycleDryRunHealthy(t *testing.T) {
	s := New("/tmp", nil, nil)
	// Cost monitor optional.
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"dry_run": true}
	res, err := s.handleStaffOpsCycle(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("error result: %+v", res)
	}
	text := toolText(res)
	for _, want := range []string{
		"Staff ops cycle:",
		"primary=",
		"T325.4",
		"Resource snapshot:",
		"dry_run=true",
		"delivered=false",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
}

func TestStaffOpsCycleCostAlertClassifiesFilePO(t *testing.T) {
	s := New("/tmp", nil, nil)
	s.SetCostMonitor(func() (*cost.Snapshot, error) {
		return &cost.Snapshot{
			Accounting:       cost.AccountingSubscription,
			GlobalUSDPerHour: 1.5,
			FleetUSDPerHour:  0.5,
			SpentTodayUSD:    3,
			Sessions:         []cost.BurnRow{{SessionID: "sess1", CostUSD: 1}},
			Alerts: []cost.Alert{
				{Kind: cost.AlertGlobalRate, Level: cost.LevelWarn, Detail: "rate high"},
			},
		}, nil
	})
	var delivered []string
	s.SetNotify(func(text string) { delivered = append(delivered, text) })

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"dry_run": false}
	res, err := s.handleStaffOpsCycle(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	text := toolText(res)
	if !strings.Contains(text, "file+PO") && !strings.Contains(text, "primary=file+PO") {
		t.Fatalf("expected file+PO path:\n%s", text)
	}
	if len(delivered) != 1 {
		t.Fatalf("deliver count=%d text=%s", len(delivered), text)
	}
	if !strings.Contains(delivered[0], "staff-ops") && !strings.Contains(delivered[0], "T325.4") {
		// FormatEventPush wraps; body should still carry report.
		if !strings.Contains(delivered[0], "Resource snapshot") {
			t.Fatalf("delivered body missing snapshot:\n%s", delivered[0])
		}
	}

	// Cooldown: second cycle with same alert should suppress file.
	delivered = nil
	res2, err := s.handleStaffOpsCycle(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	text2 := toolText(res2)
	if !strings.Contains(text2, "cooldown") && !strings.Contains(text2, "primary=ignore") {
		// After cooldown suppress, primary may be ignore.
		if strings.Contains(text2, "filed=1") {
			t.Fatalf("second cycle should not re-file:\n%s", text2)
		}
	}
}

func TestStaffOpsCycleCooldownDryRunNoDeliver(t *testing.T) {
	s := New("/tmp", nil, nil)
	s.SetCostMonitor(func() (*cost.Snapshot, error) {
		return &cost.Snapshot{
			Alerts: []cost.Alert{{Kind: "x", Level: cost.LevelWarn, Detail: "d"}},
		}, nil
	})
	var delivered []string
	s.SetNotify(func(text string) { delivered = append(delivered, text) })

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"dry_run": true}
	if _, err := s.handleStaffOpsCycle(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if len(delivered) != 0 {
		t.Fatalf("dry_run delivered: %v", delivered)
	}
	// Cooldown not marked: live cycle should still file.
	req.Params.Arguments = map[string]any{"dry_run": false}
	res, err := s.handleStaffOpsCycle(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(toolText(res), "filed=1") && !strings.Contains(toolText(res), "file+PO") {
		t.Fatalf("after dry_run, live should file:\n%s", toolText(res))
	}
}

func TestIsPOName(t *testing.T) {
	cases := map[string]bool{
		"jevons-po":     true,
		"tern-po":       true,
		"worker-a":      false,
		"jv-t99-impl":   false,
		"minicades_po":  true,
		"":              false,
	}
	for name, want := range cases {
		if got := isPOName(name); got != want {
			t.Errorf("isPOName(%q)=%v want %v", name, got, want)
		}
	}
}

func TestSampleStaffOpsCountsAgents(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(dir + "/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{
		{Name: "jevons", SessionID: "sess-overseer", Purpose: claudia.PurposeOverseer},
		{Name: "jevons-po", SessionID: "sess-po", Purpose: claudia.PurposeWork},
		{Name: "worker-1", SessionID: "sess-w1", Purpose: claudia.PurposeWork},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	s := New("/tmp", nil, nil)
	s.SetRegistry(reg)

	_, resources := s.sampleStaffOps(4)
	if resources.FrontierDepth != 4 {
		t.Fatalf("frontier=%d", resources.FrontierDepth)
	}
	// No live procs → all non-overseer count as stopped; idle PO includes jevons-po.
	if resources.StoppedAgents < 2 {
		t.Fatalf("stopped=%d", resources.StoppedAgents)
	}
	if resources.IdlePOCount < 1 {
		t.Fatalf("idlePO=%d", resources.IdlePOCount)
	}
}

func TestStaffOpsPurePolicyWiring(t *testing.T) {
	// Sanity: MCP package reuses staffops action vocabulary.
	cd := &staffops.Cooldown{Duration: time.Hour}
	res := staffops.RunCycle(staffops.CycleArgs{
		Signals: []staffops.Signal{{
			Kind: "cost_alert", Symptom: "cost:global-rate", Severity: "medium",
		}},
		Cooldown: cd,
		Now:      time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC),
	})
	if res.Primary != staffops.ActionFilePO {
		t.Fatalf("primary=%s", res.Primary)
	}
}
