// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/agentreport"
	"github.com/marcelocantos/jevons/internal/envelope"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/staffops"
)

func TestNormalizeTargetGrepID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"🎯T410", "T410"},
		{"T410", "T410"},
		{"", ""},
		{"-n", ""},
		{"T410\nfoo", ""},
	}
	for _, tc := range cases {
		if got := normalizeTargetGrepID(tc.in); got != tc.want {
			t.Errorf("normalizeTargetGrepID(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestFillIdleResidueEvidenceFromReportAndIntent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	finish := declaredFinishReport("T410", "🎯T410 done. Commit abcdef1 lands the classifier.\n`go test ./internal/staffops/ -run T410` PASS.")
	if _, err := agentreport.Save(dir, "jv-fin", finish, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	ask := "Blocked pending your answer: need one real keypress (cmd-shift-]) before I can close."
	if _, err := agentreport.Save(dir, "jv-ask", ask, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	fin := staffops.AgentObs{Name: "jv-fin", IdleResidue: true, OpenMission: true}
	fillIdleResidueEvidence(&fin, claudia.AgentDef{
		Name: "jv-fin", TargetID: "T410", WorkDir: dir,
	}, dir, fleetintent.Snapshot{})
	if !fin.ReportLooksFinished {
		t.Fatalf("finish report not seen: %+v", fin)
	}
	if fin.OwnerAskPresent {
		t.Fatalf("finish report must not set OwnerAskPresent: %+v", fin)
	}

	blk := staffops.AgentObs{Name: "jv-ask", IdleResidue: true, OpenMission: true}
	fillIdleResidueEvidence(&blk, claudia.AgentDef{
		Name: "jv-ask", TargetID: "T370", WorkDir: dir,
	}, dir, fleetintent.Snapshot{
		Agents: map[string]fleetintent.Record{
			"jv-ask": {State: fleetintent.BlockedOwner},
		},
	})
	if !blk.OwnerAskPresent || !blk.IntentBlockedOwner {
		t.Fatalf("ask/intent not seen: %+v", blk)
	}
	if blk.ReportLooksFinished {
		t.Fatalf("ask report must not look finished: %+v", blk)
	}
}

func TestT7664ReportAskIsDeclaredNotASentence(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	prose := "Blocked pending your answer: need one real keypress before I can close."
	if !strings.Contains(prose, "Blocked pending your answer") {
		t.Fatal("specimen lost the ask words")
	}
	if ClassifyReportAsk(prose) != AskNone {
		t.Fatal("a sentence is an ask")
	}
	if ReportAwaitsOverseer(prose) {
		t.Fatal("unenveloped ask sentence awaited the overseer")
	}
	if _, err := agentreport.Save(dir, "jv-prose", prose, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	proseObs := staffops.AgentObs{Name: "jv-prose", IdleResidue: true, OpenMission: true}
	fillIdleResidueEvidence(&proseObs, claudia.AgentDef{
		Name: "jv-prose", TargetID: "T7664-ask", WorkDir: dir,
	}, dir, fleetintent.Snapshot{})
	if proseObs.OwnerAskPresent {
		t.Fatalf("unenveloped ask set OwnerAskPresent: %+v", proseObs)
	}

	escalation := envelope.Format(&envelope.Message{
		Kind:    envelope.KindEscalation,
		Target:  "T7664-ask",
		Payload: prose,
	})
	if !ReportAwaitsOverseer(escalation) {
		t.Fatal("declared escalation did not await the overseer")
	}
	if _, err := agentreport.Save(dir, "jv-esc", escalation, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	esc := staffops.AgentObs{Name: "jv-esc", IdleResidue: true, OpenMission: true}
	fillIdleResidueEvidence(&esc, claudia.AgentDef{
		Name: "jv-esc", TargetID: "T7664-ask", WorkDir: dir,
	}, dir, fleetintent.Snapshot{})
	if !esc.OwnerAskPresent || esc.ReportLooksFinished {
		t.Fatalf("declared escalation: %+v", esc)
	}

	fileReq := envelope.Format(&envelope.Message{
		Kind:    envelope.KindTargetFileRequest,
		Target:  "T7664-ask",
		Name:    "typed ask",
		Payload: prose,
	})
	if !ReportAwaitsOverseer(fileReq) {
		t.Fatal("declared target-file request did not await the overseer")
	}
	if _, err := agentreport.Save(dir, "jv-file", fileReq, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	filed := staffops.AgentObs{Name: "jv-file", IdleResidue: true, OpenMission: true}
	fillIdleResidueEvidence(&filed, claudia.AgentDef{
		Name: "jv-file", TargetID: "T7664-ask", WorkDir: dir,
	}, dir, fleetintent.Snapshot{})
	if !filed.OwnerAskPresent || filed.ReportLooksFinished {
		t.Fatalf("declared target-file request: %+v", filed)
	}

	blocked := envelope.Format(&envelope.Message{
		Kind:         envelope.KindFinishReport,
		Target:       "T7664-ask",
		Status:       envelope.ProgressBlocked,
		Blocker:      "owner-keypress",
		SilentLedger: envelope.SilentLedgerEmpty,
		Payload:      prose,
	})
	if !ReportAwaitsOverseer(blocked) {
		t.Fatal("blocked finish-report did not await the overseer")
	}
	if ReportAwaitsOverseer(declaredFinishReport("T7664-ask", "Landed.")) {
		t.Fatal("oracle finish awaited the overseer")
	}
}

func TestFillIdleResidueEvidenceOwnedBy(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ledger := []byte("schema_version: \"1.0\"\ntargets:\n  T370:\n    name: keypress\n    status: converging\n    owned_by:\n      owner: marcelo\n      reason: needs real cmd-shift-]\n")
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), ledger, 0o644); err != nil {
		t.Fatal(err)
	}
	ao := staffops.AgentObs{Name: "jv-t370", IdleResidue: true, OpenMission: true}
	fillIdleResidueEvidence(&ao, claudia.AgentDef{
		Name: "jv-t370", TargetID: "T370", WorkDir: dir,
	}, "", fleetintent.Snapshot{})
	if !ao.OwnerAskPresent {
		t.Fatalf("owned_by must set OwnerAskPresent: %+v", ao)
	}
	if ao.TargetLedgerStatus != "converging" {
		t.Fatalf("status=%q", ao.TargetLedgerStatus)
	}
}

func TestHasBoundTargetCommitsThisRepo(t *testing.T) {
	t.Parallel()
	// Live against this checkout: T410 exists in bullseye history after filing.
	// Absence is not a failure of the helper — only a false positive is.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// Walk up to repo root from internal/mcpserver.
	root := filepath.Clean(filepath.Join(cwd, "../.."))
	if !hasBoundTargetCommits(root, "T999999-nonexistent-target-id") {
		return
	}
	t.Fatal("nonexistent target id matched a commit")
}
