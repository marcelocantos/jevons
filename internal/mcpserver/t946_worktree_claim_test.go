// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/gate"
)

// 🎯T946: a report with no cited SHA and no cited gate id must never score
// finished_work when the worker's own worktree carries uncommitted tracked
// changes or new untracked source files — the oracle-marker words ("pass",
// "green", "go test") describe nothing that was actually landed.
//
// Two real incidents, both reaped on a stray completion word while their
// worktree was fully uncommitted:
//
//	jv-t906-accept-lifts-block — claim_marker "done", matched span "That
//	stash is my own reversion (expected, done deliberately to verify red)."
//	jv-t943-stale-token-reload — claim_marker "complete", matched span "I'll
//	wait for the full test run to complete before committing."
//
// Fixtures are the verbatim reports (internal/mcpserver/testdata/t946_*).

func t946Fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(b)
}

// gitRun runs a git command in dir, failing the test on error.
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// seededGitWorktree makes a one-commit git repo, then optionally dirties it
// with an uncommitted tracked edit or a new untracked source file.
type dirtyShape int

const (
	dirtyNone dirtyShape = iota
	dirtyTrackedEdit
	dirtyUntrackedFile
)

func seededGitWorktree(t *testing.T, shape dirtyShape) string {
	t.Helper()
	dir := t.TempDir()
	gitInit(t, dir)
	gitRun(t, dir, "config", "user.email", "test@example.com")
	gitRun(t, dir, "config", "user.name", "Test")
	seed := filepath.Join(dir, "seed.go")
	if err := os.WriteFile(seed, []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "seed.go")
	gitRun(t, dir, "commit", "-m", "seed")
	switch shape {
	case dirtyTrackedEdit:
		if err := os.WriteFile(seed, []byte("package x\n\nfunc Changed() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	case dirtyUntrackedFile:
		if err := os.WriteFile(filepath.Join(dir, "new_thing.go"), []byte("package x\n\nfunc NewThing() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// regWithWorkerDir is regWithTree with the worker's WorkDir overridden, so
// the reap path's gate.ProbeTree call reads the caller's own worktree.
func regWithWorkerDir(t *testing.T, workerDir string) *claudia.Registry {
	t.Helper()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{
		{Name: "jevons", WorkDir: dir, SessionID: "s-o", Materialized: true, Provider: "grok"},
		{Name: "po", WorkDir: dir, SessionID: "s-po", Materialized: true, Provider: "grok", Parent: "jevons"},
		{Name: "worker", WorkDir: workerDir, SessionID: "s-w", Materialized: true, Provider: "grok", Parent: "po"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	return reg
}

// The two real incidents: neither must reap, over a dirty worktree, with no
// registry at all needed to reproduce the bug — the phrase-level fix
// (explicitIncompleteMarkers) catches both before the worktree is even
// consulted.
func TestT946IncidentReportsAreNotFinishedWork(t *testing.T) {
	cases := []struct {
		name string
		file string
		deny string // the phrase that must veto the reap
	}{
		{
			name: "jv-t906-accept-lifts-block",
			file: "t946_jv_t906_accept_lifts_block_report.md",
			deny: "done deliberately to verify red",
		},
		{
			name: "jv-t943-stale-token-reload",
			file: "t946_jv_t943_stale_token_reload_report.md",
			deny: "i'll wait for the full test run to complete",
		},
	}
	reg := regWithWorkerDir(t, seededGitWorktree(t, dirtyUntrackedFile))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := t946Fixture(t, tc.file)
			lower := strings.ToLower(report)
			if !strings.Contains(lower, tc.deny) {
				t.Fatalf("fixture lost its incident sentence %q", tc.deny)
			}
			if !hasCompletionClaim(asciiLower(report)) {
				t.Fatal("fixture no longer carries a completion word — cannot reproduce the incident")
			}
			if reportCitesOracleID(report) {
				t.Fatal("fixture unexpectedly cites a SHA or gate id — no longer the uncited shape")
			}
			if LooksLikeFinishedWorkReport(report) {
				t.Fatal("incident report classified as finished_work")
			}
			ok, reason := ShouldAutoReapDoneWorkAgent(reg, "worker", report, func(string) bool { return false })
			if ok {
				t.Fatalf("incident report reaps (reason %s)", reason)
			}
			if !strings.HasPrefix(reason, "awaits_overseer_") {
				t.Fatalf("reason = %q, want awaits_overseer_* (the phrase-level veto)", reason)
			}
		})
	}
}

// The worktree-vs-claim veto in isolation: a report that clears every
// existing ask/finish-shape check (no explicit-incomplete phrase, no
// checkpoint, no closing question) must still not reap finished_work when it
// cites no SHA/gate id and the worker's worktree is dirty.
func TestT946UncitedOracleClaimOverDirtyWorktreeNeverReaps(t *testing.T) {
	report := "Implemented the fix. All tests pass. Done."
	if !LooksLikeFinishedWorkReport(report) {
		t.Fatal("control must look like finished_work before the T946 veto — otherwise this is not testing the new check")
	}
	if reportCitesOracleID(report) {
		t.Fatal("control must not cite a SHA or gate id")
	}

	for _, shape := range []dirtyShape{dirtyTrackedEdit, dirtyUntrackedFile} {
		reg := regWithWorkerDir(t, seededGitWorktree(t, shape))
		ok, reason := ShouldAutoReapDoneWorkAgent(reg, "worker", report, func(string) bool { return false })
		if ok {
			t.Fatalf("shape %v: uncited oracle-marker claim reaped over a dirty worktree", shape)
		}
		if reason != "false_green_uncited_claim_dirty_worktree" {
			t.Fatalf("shape %v: reason = %q, want false_green_uncited_claim_dirty_worktree", shape, reason)
		}
	}
}

// Control: the same uncited claim over a CLEAN worktree still reaps — the
// veto is about contradicted evidence, not a blanket refusal of prose-only
// oracle markers.
func TestT946UncitedOracleClaimOverCleanWorktreeStillReaps(t *testing.T) {
	report := "Implemented the fix. All tests pass. Done."
	reg := regWithWorkerDir(t, seededGitWorktree(t, dirtyNone))
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, "worker", report, func(string) bool { return false })
	if !ok {
		t.Fatalf("clean-worktree control did not reap (reason %s)", reason)
	}
}

// Control: a cited SHA over a dirty worktree still reaps — citing an actual
// artifact is exactly what the veto asks for, and shaevidence's own
// extraction (🎯T427) recognises it.
func TestT946CitedSHAOverDirtyWorktreeStillReaps(t *testing.T) {
	report := "Implemented the fix. All tests pass. Done. Committed abcdef0123456."
	if !reportCitesOracleID(report) {
		t.Fatal("control must cite a SHA")
	}
	reg := regWithWorkerDir(t, seededGitWorktree(t, dirtyTrackedEdit))
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, "worker", report, func(string) bool { return false })
	if !ok {
		t.Fatalf("cited-SHA control did not reap (reason %s)", reason)
	}
}

// Direct unit coverage of the pure classifier, no registry or reap path.
func TestT946UncitedClaimAgainstDirtyWorktree(t *testing.T) {
	dirtyTree := &gate.TreeProvenance{Commit: "abc123", Clean: false, DirtyFiles: 2}
	cleanTree := &gate.TreeProvenance{Commit: "abc123", Clean: true}
	claim := "All tests pass. Done."
	cited := "All tests pass. Done. SHA abcdef0123456."

	if UncitedClaimAgainstDirtyWorktree(claim, nil) {
		t.Fatal("nil tree (unknown provenance) must not veto")
	}
	if UncitedClaimAgainstDirtyWorktree(claim, cleanTree) {
		t.Fatal("clean tree must not veto")
	}
	if !UncitedClaimAgainstDirtyWorktree(claim, dirtyTree) {
		t.Fatal("uncited marker-only claim over a dirty tree must veto")
	}
	if UncitedClaimAgainstDirtyWorktree(cited, dirtyTree) {
		t.Fatal("a cited SHA must clear the veto even over a dirty tree")
	}
}
