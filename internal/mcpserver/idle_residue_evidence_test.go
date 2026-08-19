// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/agentreport"
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
	finish := "🎯T410 done. Commit abcdef1 lands the classifier.\n`go test ./internal/staffops/ -run T410` PASS."
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
