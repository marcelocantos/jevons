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
	"github.com/marcelocantos/jevons/internal/roles"
	"github.com/marcelocantos/jevons/internal/staffops"
)

const t1192Ledger = `
schema_version: 1
project: test
targets:
  T1191:
    name: integrate gate specimen
    status: identified
    acceptance: ["land"]
`

func t1192Fixture(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte(t1192Ledger), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.SetAgentReportDir(state)
	s.idleActivity = NewIdleActivityTracker()
	if err := s.OpenRoleAssignments(dir); err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{
		{Name: "jevons-po", WorkDir: dir, SessionID: "po-1", Parent: "jevons", Purpose: purposeProductOwner},
		{Name: "jv-auditor-po", WorkDir: dir, SessionID: "aud-1", Parent: "jevons-po", Purpose: claudia.PurposeWork},
		{Name: "jv-t1191-auto", WorkDir: dir, SessionID: "w-1", Parent: "jevons-po", Purpose: claudia.PurposeWork, TargetID: "T1191"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.recordAgentRole("jv-auditor-po", roles.Auditor); err != nil {
		t.Fatal(err)
	}
	finish := declaredFinishReport("T1191", "landed integrate slice")
	if _, err := agentreport.Save(state, "jv-t1191-auto", finish, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func findPOAuditorStall(sigs []staffops.Signal, po string) *staffops.Signal {
	for i := range sigs {
		if sigs[i].Kind == "po_auditor_mutual_wait" && strings.Contains(sigs[i].Symptom, po) {
			return &sigs[i]
		}
	}
	return nil
}

func TestT1192SentinelDetectsPOAuditorMutualWait(t *testing.T) {
	alive := map[string]bool{"jevons-po": true, "jv-auditor-po": true, "jv-t1191-auto": true}
	s, dir := t1192Fixture(t)
	setPhase(s, "jevons-po", "idle", time.Time{})
	setPhase(s, "jv-auditor-po", "idle", time.Time{})

	t0 := time.Date(2026, 10, 22, 23, 8, 0, 0, time.UTC)
	if sig := findPOAuditorStall(sampleAt(s, dir, alive, t0), "jevons-po"); sig != nil {
		t.Fatalf("first sample must start grace, not fire: %+v", sig)
	}
	past := t0.Add(DefaultSentinelMechanicalGrace + time.Minute)
	sig := findPOAuditorStall(sampleAt(s, dir, alive, past), "jevons-po")
	if sig == nil {
		t.Fatal("expected po_auditor_mutual_wait stall")
	}
	if !strings.Contains(sig.Detail, "jv-t1191-auto") {
		t.Fatalf("detail must cite handoff worker: %q", sig.Detail)
	}

	res, _ := s.runSentinelCycle(SentinelLoopArgs{
		Server: s, Workdir: dir, DryRun: true,
		ProcessRunning: func(name string) bool { return alive[name] },
		Now:            func() time.Time { return past.Add(time.Minute) },
	})
	for _, d := range res.Decisions {
		if d.Signal.Kind == "po_auditor_mutual_wait" && d.Action != staffops.ActionFilePO {
			t.Fatalf("want file+PO; decision=%+v", d)
		}
	}
	if !strings.Contains(staffops.FormatPOMission(res), "jv-t1191-auto") {
		t.Fatalf("PO mission must list handoffs:\n%s", staffops.FormatPOMission(res))
	}
}

func TestT1192AgentListSurfacesFault(t *testing.T) {
	alive := map[string]bool{"jevons-po": true, "jv-auditor-po": true, "jv-t1191-auto": true}
	s, dir := t1192Fixture(t)
	setPhase(s, "jevons-po", "idle", time.Time{})
	setPhase(s, "jv-auditor-po", "idle", time.Time{})
	t0 := time.Date(2026, 10, 22, 23, 8, 0, 0, time.UTC)
	sampleAt(s, dir, alive, t0)
	past := t0.Add(DefaultSentinelMechanicalGrace + time.Minute)
	sampleAt(s, dir, alive, past)

	list, err := observedAgentList(s, context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	body := toolText(list)
	if !strings.Contains(body, AgentStatusPOAuditorMutualWait) {
		t.Fatalf("agent_list status missing typed fault:\n%s", body)
	}
	if !strings.Contains(body, "jv-t1191-auto") {
		t.Fatalf("agent_list must name pending handoff:\n%s", body)
	}
}
