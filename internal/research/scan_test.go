// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package research

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/rsi"
)

func TestRepoFindingsQuantizeVolumeAndNameHead(t *testing.T) {
	commits := []rsi.GitCommit{
		{SHA: "aaaaaaaabbbb", Subject: "Add research cycle", At: at(30), Files: []string{"internal/research/agent.go", "web/index.html"}},
		{SHA: "ccccccccdddd", Subject: "Fix scan", At: at(20), Files: []string{"internal/research/scan.go"}},
	}
	findings := RepoFindings("jevons", commits, true)
	if len(findings) != 3 {
		t.Fatalf("want repo + 2 scopes, got %d: %+v", len(findings), findings)
	}
	if findings[0].Key != "repo:jevons" || !strings.Contains(findings[0].Claim, "aaaaaaaa") {
		t.Fatalf("repo finding must name the head: %+v", findings[0])
	}
	byKey := map[string]Finding{}
	for _, f := range findings {
		byKey[f.Key] = f
	}
	rsiScope, ok := byKey["scope:internal/research"]
	if !ok {
		t.Fatalf("want internal/research scope, got %+v", byKey)
	}
	if !strings.Contains(rsiScope.Claim, levelLight) {
		t.Fatalf("2 commits should read as %s: %q", levelLight, rsiScope.Claim)
	}
	if !strings.Contains(rsiScope.Claim, "aaaaaaaa") {
		t.Fatalf("scope head must be the newest commit touching it: %q", rsiScope.Claim)
	}

}

// A cycle that counts one more commit inside the same activity band must
// restate the same claim — that is what keeps a standing cycle quiet instead of
// superseding its own note on every tick.
func TestRepoFindingsClaimIsStableWithinActivityBand(t *testing.T) {
	head := rsi.GitCommit{SHA: "aaaaaaaabbbb", Subject: "Add research cycle", At: at(50), Files: []string{"internal/research/agent.go"}}
	four := []rsi.GitCommit{head,
		{SHA: "b1", Subject: "b", At: at(40), Files: []string{"internal/research/scan.go"}},
		{SHA: "c1", Subject: "c", At: at(30), Files: []string{"internal/research/feed.go"}},
		{SHA: "d1", Subject: "d", At: at(20), Files: []string{"internal/research/note.go"}},
	}
	five := append(append([]rsi.GitCommit(nil), four...),
		rsi.GitCommit{SHA: "e1", Subject: "e", At: at(10), Files: []string{"internal/research/store.go"}})

	before := RepoFindings("jevons", four, true)
	after := RepoFindings("jevons", five, true)
	if before[0].Claim != after[0].Claim {
		t.Fatalf("same band + same head must yield the same claim:\n%q\n%q", before[0].Claim, after[0].Claim)
	}
	if before[0].Evidence[0] == after[0].Evidence[0] {
		t.Fatal("evidence should still record the exact count")
	}
}

func TestRepoFindingsRelatedRepoIsRepoLevelOnly(t *testing.T) {
	findings := RepoFindings("claudia", []rsi.GitCommit{
		{SHA: "1234567890", Subject: "release", At: at(5), Files: []string{"cmd/claudia/main.go"}},
	}, false)
	if len(findings) != 1 || findings[0].Key != "repo:claudia" {
		t.Fatalf("related repos stay coarse, got %+v", findings)
	}
}

const bullseyeFixture = `
schema_version: 5
targets:
  T1:
    name: Locked down
    status: achieved
  T99:
    name: Older leaf
    status: identified
  T356:
    name: Ambient research agents
    status: identified
  T356.1:
    name: Sub leaf
    status: converging
  T7:
    name: Parked
    status: set_aside
`

func TestFrontierFindingsCountAndOrderTargets(t *testing.T) {
	findings, count, err := FrontierFindings([]byte(bullseyeFixture))
	if err != nil {
		t.Fatalf("FrontierFindings: %v", err)
	}
	if count != 5 {
		t.Fatalf("want 5 targets, got %d", count)
	}
	if len(findings) != 2 {
		t.Fatalf("want status + newest findings, got %+v", findings)
	}
	if !strings.Contains(findings[0].Claim, "2 identified") || !strings.Contains(findings[0].Claim, "1 converging") {
		t.Fatalf("status claim wrong: %q", findings[0].Claim)
	}
	if !strings.Contains(findings[0].Claim, "1 set aside") {
		t.Fatalf("status claim must count set_aside: %q", findings[0].Claim)
	}
	// Numeric order, not lexical: T356 outranks T99.
	if !strings.HasPrefix(findings[1].Claim, "newest identified targets: 🎯T356, 🎯T99") {
		t.Fatalf("newest ordering wrong: %q", findings[1].Claim)
	}
}

func TestFrontierFindingsRejectsMalformedLedger(t *testing.T) {
	if _, _, err := FrontierFindings([]byte("targets: [oops")); err == nil {
		t.Fatal("malformed ledger must be a hard error, not a silent empty scan")
	}
}

func TestEventFindingsClusterByComponentAndOutcome(t *testing.T) {
	rows := []rsi.EventRow{
		{TS: "2026-08-09T10:30:00Z", Component: "butler", Decision: "spawn", Outcome: "error"},
		{TS: "2026-08-09T10:20:00Z", Component: "butler", Decision: "spawn", Outcome: "error"},
		{TS: "2026-08-09T10:10:00Z", Component: "butler", Decision: "spawn", Outcome: "error"},
		{TS: "2026-08-09T10:05:00Z", Component: "fleet", Decision: "reap", Outcome: ""},
		{Component: "", Decision: "ignored"},
	}
	findings := EventFindings(rows)
	if len(findings) != 2 {
		t.Fatalf("want 2 clusters (blank component dropped), got %+v", findings)
	}
	if findings[0].Key != "events:butler:error" || !strings.Contains(findings[0].Claim, levelSteady) {
		t.Fatalf("busiest cluster first with quantized level: %+v", findings[0])
	}
	if !strings.Contains(findings[1].Claim, "unlabelled") {
		t.Fatalf("blank outcome should read as unlabelled: %q", findings[1].Claim)
	}
}

func TestSessionFindingsSummarizeRecentSessions(t *testing.T) {
	turns := []rsi.ChatTurn{
		{SourceID: "sess-a", TS: at(10)},
		{SourceID: "sess-a", TS: at(20)},
		{SourceID: "sess-b", TS: at(40)},
	}
	findings := SessionFindings(turns)
	if len(findings) != 1 || findings[0].Key != "sessions:recent" {
		t.Fatalf("want one session finding, got %+v", findings)
	}
	if !strings.Contains(findings[0].Claim, "sess-b") || !strings.Contains(findings[0].Claim, "2 sessions") {
		t.Fatalf("claim should name session count and newest: %q", findings[0].Claim)
	}
	if SessionFindings(nil) != nil {
		t.Fatal("no turns must produce no findings")
	}
}

func TestDiscoverRelatedReposSkipsPrimaryAndNonRepos(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"jevons", "claudia", "notes"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"jevons", "claudia"} {
		if err := os.MkdirAll(filepath.Join(root, name, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got := DiscoverRelatedRepos([]string{root}, filepath.Join(root, "jevons"), 10)
	if len(got) != 1 || filepath.Base(got[0]) != "claudia" {
		t.Fatalf("want only claudia, got %v", got)
	}
	if bounded := DiscoverRelatedRepos([]string{root}, "", 1); len(bounded) != 1 {
		t.Fatalf("max must bound discovery, got %v", bounded)
	}
	if none := DiscoverRelatedRepos([]string{filepath.Join(root, "missing")}, "", 5); len(none) != 0 {
		t.Fatalf("missing root must be skipped, got %v", none)
	}
}

func TestScanReadsFrontierWithoutGitRepository(t *testing.T) {
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "bullseye.yaml"), []byte(bullseyeFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Scan(ScanArgs{Workdir: work, Now: at(0), Since: at(0).Add(-time.Hour)})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(res.Inputs) != 1 || res.Inputs[0].Topic != "context/frontier" {
		t.Fatalf("want a frontier input, got %+v", res.Inputs)
	}
	if res.Stats.FrontierIDs != 5 {
		t.Fatalf("want 5 frontier ids in stats, got %d", res.Stats.FrontierIDs)
	}
	if res.Stats.Repos != 0 {
		t.Fatalf("a non-git workdir must not count as a scanned repo, got %d", res.Stats.Repos)
	}
}
