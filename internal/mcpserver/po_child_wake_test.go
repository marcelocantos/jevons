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

	"github.com/marcelocantos/jevons/internal/agentreport"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/fleetlog"
)

const t1193Ledger = `
schema_version: 1
project: test
targets:
  T1193:
    name: idle PO woken by child reports
    status: identified
    acceptance: ["wake"]
`

func t1193Fixture(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte(t1193Ledger), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.SetFleetIntentStore(store)
	s.SetAgentReportDir(state)
	s.SetRemovalAccount(fleetlog.New(nil))
	s.idleActivity = NewIdleActivityTracker()
	if err := s.OpenRoleAssignments(dir); err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{
		{Name: "jevons-po", WorkDir: dir, SessionID: "po-1", Parent: "jevons", Purpose: purposeProductOwner},
		{Name: "jv-t1193-worker", WorkDir: dir, SessionID: "w-1", Parent: "jevons-po", Purpose: claudia.PurposeWork, TargetID: "T1193"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	return s, dir
}

func TestT1193WakesPOWithUnactedChildReport(t *testing.T) {
	s, dir := t1193Fixture(t)
	alive := map[string]bool{"jevons-po": true, "jv-t1193-worker": true}
	finish := declaredFinishReport("T1193", "landed slice")
	if _, err := agentreport.Save(s.agentReportStateDir(), "jv-t1193-worker", finish, time.Date(2026, 10, 23, 2, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	setPhase(s, "jevons-po", "idle", time.Time{})
	t0 := time.Date(2026, 10, 23, 2, 5, 0, 0, time.UTC)
	var delivered []string
	s.SetNotify(func(text string) { delivered = append(delivered, text) })
	_, _ = s.runSentinelCycle(SentinelLoopArgs{
		Server: s, Workdir: dir, DryRun: false,
		ProcessRunning: func(name string) bool { return alive[name] },
		Now:            func() time.Time { return t0 },
	})
	if len(delivered) != 0 {
		t.Fatalf("first cycle within grace must not wake PO: %v", delivered)
	}
	past := t0.Add(DefaultSentinelMechanicalGrace + time.Minute)
	_, _ = s.runSentinelCycle(SentinelLoopArgs{
		Server: s, Workdir: dir, DryRun: false,
		ProcessRunning: func(name string) bool { return alive[name] },
		Now:            func() time.Time { return past },
	})
	if len(delivered) != 1 {
		t.Fatalf("want one wake, got %d: %v", len(delivered), delivered)
	}
	for _, want := range []string{"jv-t1193-worker", "finish-report", "T1193", "report_id="} {
		if !strings.Contains(delivered[0], want) {
			t.Fatalf("wake missing %q:\n%s", want, delivered[0])
		}
	}
	// Same report must not wake twice before PO acts.
	_, _ = s.runSentinelCycle(SentinelLoopArgs{
		Server: s, Workdir: dir, DryRun: false,
		ProcessRunning: func(name string) bool { return alive[name] },
		Now:            func() time.Time { return past.Add(time.Minute) },
	})
	if len(delivered) != 1 {
		t.Fatalf("duplicate wake before PO turn: %d messages", len(delivered))
	}
	afterAct := past.Add(2 * time.Minute)
	s.reportDeliveryNow = func() time.Time { return afterAct }
	s.notePOTurnEndedForChildWake("jevons-po")
	_, _ = s.runSentinelCycle(SentinelLoopArgs{
		Server: s, Workdir: dir, DryRun: false,
		ProcessRunning: func(name string) bool { return alive[name] },
		Now:            func() time.Time { return afterAct },
	})
	if len(delivered) != 1 {
		t.Fatalf("acted PO must not be re-woken for same report: %d", len(delivered))
	}
}

func TestT1193ReapedChildReportStillWakesPO(t *testing.T) {
	s, dir := t1193Fixture(t)
	alive := map[string]bool{"jevons-po": true}
	finish := declaredFinishReport("T1193", "scout handoff landed")
	if _, err := agentreport.Save(s.agentReportStateDir(), "jv-t1193-worker", finish, time.Date(2026, 10, 23, 3, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	removed, err := s.RemovalAccount().Remove(s.registry, "jv-t1193-worker", fleetlog.Removal{
		Reason: fleetlog.ReasonReapDone,
		Detail: "reaped after finish",
	})
	if err != nil || !removed {
		t.Fatalf("remove worker: removed=%v err=%v", removed, err)
	}
	s.MarkAgentReaped("jv-t1193-worker", "product:reap_done", "reaped after finish")
	setPhase(s, "jevons-po", "idle", time.Time{})
	var delivered []string
	s.SetNotify(func(text string) { delivered = append(delivered, text) })
	t0 := time.Date(2026, 10, 23, 3, 10, 0, 0, time.UTC)
	_, _ = s.runSentinelCycle(SentinelLoopArgs{
		Server: s, Workdir: dir, DryRun: false,
		ProcessRunning: func(name string) bool { return alive[name] },
		Now:            func() time.Time { return t0 },
	})
	past := t0.Add(DefaultSentinelMechanicalGrace + time.Minute)
	_, _ = s.runSentinelCycle(SentinelLoopArgs{
		Server: s, Workdir: dir, DryRun: false,
		ProcessRunning: func(name string) bool { return alive[name] },
		Now:            func() time.Time { return past },
	})
	var wake string
	for _, msg := range delivered {
		if strings.Contains(msg, "T1193") && strings.Contains(msg, "jv-t1193-worker") {
			wake = msg
			break
		}
	}
	if wake == "" {
		t.Fatalf("reaped child must wake PO: %v", delivered)
	}
}


func TestT1193NonTerminalReportDoesNotWake(t *testing.T) {
	s, dir := t1193Fixture(t)
	alive := map[string]bool{"jevons-po": true, "jv-t1193-worker": true}
	block := "jevons: kind blocked-on-owner\njevons: target T1193\nblocked on owner decision"
	if _, err := agentreport.Save(s.agentReportStateDir(), "jv-t1193-worker", block, time.Date(2026, 10, 23, 4, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	setPhase(s, "jevons-po", "idle", time.Time{})
	var delivered []string
	s.SetNotify(func(text string) { delivered = append(delivered, text) })
	past := time.Date(2026, 10, 23, 4, 5, 0, 0, time.UTC).Add(DefaultSentinelMechanicalGrace + time.Minute)
	_, _ = s.runSentinelCycle(SentinelLoopArgs{
		Server: s, Workdir: dir, DryRun: false,
		ProcessRunning: func(name string) bool { return alive[name] },
		Now:            func() time.Time { return past },
	})
	for _, msg := range delivered {
		if strings.Contains(msg, "T1193") && strings.Contains(msg, "report_id=") {
			t.Fatalf("non-terminal report must not wake PO: %s", msg)
		}
	}
}

func TestT1193ParentReportOfferedDedup(t *testing.T) {
	s, dir := t1193Fixture(t)
	alive := map[string]bool{"jevons-po": true, "jv-t1193-worker": true}
	finish := declaredFinishReport("T1193", "landed slice")
	rec, err := agentreport.Save(s.agentReportStateDir(), "jv-t1193-worker", finish, time.Date(2026, 10, 23, 5, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	s.noteParentReportOffered("jevons-po", rec.ID)
	setPhase(s, "jevons-po", "idle", time.Time{})
	var delivered []string
	s.SetNotify(func(text string) { delivered = append(delivered, text) })
	past := time.Date(2026, 10, 23, 5, 5, 0, 0, time.UTC).Add(DefaultSentinelMechanicalGrace + time.Minute)
	_, _ = s.runSentinelCycle(SentinelLoopArgs{
		Server: s, Workdir: dir, DryRun: false,
		ProcessRunning: func(name string) bool { return alive[name] },
		Now:            func() time.Time { return past },
	})
	for _, msg := range delivered {
		if strings.Contains(msg, "jv-t1193-worker") {
			t.Fatalf("T731 offered report must not re-wake: %s", msg)
		}
	}
}

func TestT1193AgentListSurfacesFault(t *testing.T) {
	s, dir := t1193Fixture(t)
	alive := map[string]bool{"jevons-po": true, "jv-t1193-worker": true}
	finish := declaredFinishReport("T1193", "landed slice")
	if _, err := agentreport.Save(s.agentReportStateDir(), "jv-t1193-worker", finish, time.Date(2026, 10, 23, 6, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	setPhase(s, "jevons-po", "idle", time.Time{})
	t0 := time.Date(2026, 10, 23, 6, 5, 0, 0, time.UTC)
	sampleAt(s, dir, alive, t0)
	past := t0.Add(DefaultSentinelMechanicalGrace + time.Minute)
	sampleAt(s, dir, alive, past)
	list, err := observedAgentList(s, context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	body := toolText(list)
	if !strings.Contains(body, AgentStatusPOUnactedChildReports) {
		t.Fatalf("agent_list status missing typed fault:\n%s", body)
	}
}
