// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"strings"
	"testing"
)

// 🎯T718: a dirty run says so in the GATE line while the runner can still
// act, not only later when T397 flags the finish report.
//
// Hermetic: a fixture dirtied with a foreign path runs a passing command;
// the GATE line carries DIRTY + tree=dirty + hint=-clean; the same command
// in a clean tree is unmarked GREEN; restoring a bare GREEN on the dirty
// tree is the mutation this file exists to catch.

func TestT718ApplyTreeVerdictDemotesOnlyGreen(t *testing.T) {
	dirty := &TreeProvenance{Commit: "abc", DirtyFiles: 2}
	clean := &TreeProvenance{Commit: "abc", Clean: true}
	for _, tc := range []struct {
		name string
		v    Verdict
		tree *TreeProvenance
		want Verdict
	}{
		{"green dirty", VerdictGreen, dirty, VerdictDirty},
		{"green clean", VerdictGreen, clean, VerdictGreen},
		{"green unknown", VerdictGreen, nil, VerdictGreen},
		{"red dirty", VerdictRed, dirty, VerdictRed},
		{"suspect dirty", VerdictSuspect, dirty, VerdictSuspect},
		{"killed dirty", VerdictKilled, dirty, VerdictKilled},
		{"unknown dirty", VerdictUnknown, dirty, VerdictUnknown},
		{"empty dirty", VerdictEmpty, dirty, VerdictEmpty},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := applyTreeVerdict(tc.v, tc.tree); got != tc.want {
				t.Fatalf("applyTreeVerdict(%s) = %s, want %s", tc.v, got, tc.want)
			}
		})
	}
}

func TestT718DirtyTreePassingCommandPrintsDirtyAndCleanHint(t *testing.T) {
	root, head := fixtureRepo(t, true)
	// Foreign path: uncommitted, not a file the command reads.
	writeFixture(t, root, "neighbour-wip.txt", "someone else's hunk\n")
	store := storeAt(t)

	rec, err := Run(&RunArgs{
		Command: []string{"true"}, Dir: root, Store: store,
		Stdout: nil, Stderr: nil,
	})
	if err != nil {
		t.Fatalf("dirty run: %v", err)
	}
	if rec.Status() != "0" {
		t.Fatalf("passing command exited %s", rec.Status())
	}
	if rec.Verdict.IsGreen() {
		t.Fatalf("dirty pass attested as a green: %s", rec.Summary())
	}
	if rec.Verdict != VerdictDirty {
		t.Fatalf("verdict = %s, want %s", rec.Verdict, VerdictDirty)
	}
	line := rec.Attestation()
	if strings.Contains(line, "exit=0 GREEN") {
		t.Fatalf("mutation: dirty tree restored a bare GREEN: %s", line)
	}
	if !strings.Contains(line, "exit=0 DIRTY") {
		t.Fatalf("GATE line missing DIRTY verdict: %s", line)
	}
	if !strings.Contains(line, "tree=dirty+") {
		t.Fatalf("GATE line missing dirty marker: %s", line)
	}
	if !strings.Contains(line, DirtyHintToken) {
		t.Fatalf("GATE line missing %s: %s", DirtyHintToken, line)
	}
	if !strings.Contains(line, "-clean") {
		t.Fatalf("GATE line does not name -clean: %s", line)
	}
	sum := rec.Summary()
	if !strings.Contains(sum, "bin/gate -clean") {
		t.Fatalf("dirty-run message does not name bin/gate -clean:\n%s", sum)
	}
	if rec.Tree == nil || rec.Tree.Commit != head {
		t.Fatalf("tree provenance %+v, want commit %s", rec.Tree, head)
	}
}

func TestT718CleanTreePassingCommandPrintsUnmarkedGreen(t *testing.T) {
	root, _ := fixtureRepo(t, true)
	store := storeAt(t)

	rec, err := Run(&RunArgs{
		Command: []string{"true"}, Dir: root, Store: store,
		Stdout: nil, Stderr: nil,
	})
	if err != nil {
		t.Fatalf("clean run: %v", err)
	}
	if rec.Verdict != VerdictGreen || rec.Status() != "0" {
		t.Fatalf("clean pass = %s, want GREEN exit 0", rec.Summary())
	}
	line := rec.Attestation()
	if !strings.Contains(line, "exit=0 GREEN") {
		t.Fatalf("clean GATE line missing unmarked GREEN: %s", line)
	}
	if strings.Contains(line, "DIRTY") {
		t.Fatalf("clean GATE line marked DIRTY: %s", line)
	}
	if strings.Contains(line, "tree=dirty+") {
		t.Fatalf("clean GATE line carries dirty marker: %s", line)
	}
	if strings.Contains(line, DirtyHintToken) {
		t.Fatalf("clean GATE line carries -clean hint: %s", line)
	}
	if strings.Contains(rec.Summary(), "bin/gate -clean") {
		t.Fatalf("clean run named -clean:\n%s", rec.Summary())
	}
}

func TestT718ReportCitingDirtyGateIsFlaggedDirtyTree(t *testing.T) {
	root, head := fixtureRepo(t, true)
	writeFixture(t, root, "neighbour-wip.txt", "someone else's hunk\n")
	store := storeAt(t)

	rec, err := Run(&RunArgs{
		Command: []string{"true"}, Dir: root, Store: store,
	})
	if err != nil {
		t.Fatalf("dirty run: %v", err)
	}
	report := "🎯T718 done. Commit `" + head[:7] + "` on local master.\n\n" +
		"    " + rec.Attestation() + "\n"
	flags := FlagFalseGreen(report, store.Lookup)
	var got *Flag
	for i := range flags {
		if flags[i].Kind == FlagDirtyTreeGate {
			got = &flags[i]
		}
	}
	if got == nil {
		t.Fatalf("citing a DIRTY gate for a commit was not dirty_tree_gate; flags=%v", flags)
	}
	if !strings.Contains(got.Detail, "bin/gate -clean") {
		t.Errorf("flag detail does not name bin/gate -clean: %s", got.Detail)
	}
}

func TestT718DirtyLineWithoutCommitClaimIsSilent(t *testing.T) {
	root, _ := fixtureRepo(t, true)
	writeFixture(t, root, "neighbour-wip.txt", "someone else's hunk\n")
	store := storeAt(t)

	rec, err := Run(&RunArgs{
		Command: []string{"true"}, Dir: root, Store: store,
	})
	if err != nil {
		t.Fatalf("dirty run: %v", err)
	}
	report := "Read of the current tree:\n" + rec.Attestation()
	for _, f := range FlagFalseGreen(report, store.Lookup) {
		t.Errorf("flagged a tree reading with no commit claim: %s", f)
	}
}

func TestT718ParseAttestationsRoundTripDirty(t *testing.T) {
	rec := &Record{
		ID: "a1b2c3d4", Name: "true", ExitStatus: 0, StatusKnown: true,
		Verdict: VerdictDirty, OutputSHA256: strings.Repeat("b", 64),
		Tree:    &TreeProvenance{Commit: "deadbeefdead", DirtyFiles: 3},
		Command: []string{"true"},
	}
	cited := ParseAttestations(rec.Attestation())
	if len(cited) != 1 {
		t.Fatalf("parsed %d attestations, want 1 from %s", len(cited), rec.Attestation())
	}
	if cited[0].Verdict != VerdictDirty || cited[0].ID != rec.ID || !cited[0].StatusIsZero() {
		t.Fatalf("round trip lost DIRTY fields: %+v", cited[0])
	}
}
