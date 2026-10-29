// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T440 //worktreereap:exempt — hermetic T1311 fixtures torn down in this
// process; a timeout/SIGKILL cannot leak them past t.TempDir.
package mcpserver

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/envelope"
	"github.com/marcelocantos/jevons/internal/worktree"
)

func t1311Scratch(t *testing.T) (clone, improvised, tip string) {
	t.Helper()
	parent := t.TempDir()
	clone = filepath.Join(parent, "wordfreq")
	if err := os.Mkdir(clone, 0o755); err != nil {
		t.Fatal(err)
	}
	t1122GitRun(t, clone, "init", "-q")
	t1122GitRun(t, clone, "checkout", "-q", "-b", "master")
	if err := os.WriteFile(filepath.Join(clone, "README"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t1122GitRun(t, clone, "add", ".")
	t1122GitRun(t, clone, "commit", "-q", "-m", "seed")
	tip = t1122GitRun(t, clone, "rev-parse", "HEAD")
	improvised = filepath.Join(parent, "wordfreq-json")
	t1122GitRun(t, clone, "worktree", "add", "-b", "feature/json", improvised)
	t.Cleanup(func() {
		_ = exec.Command("git", "-C", clone, "worktree", "remove", "--force", improvised).Run()
		_ = exec.Command("git", "-C", clone, "branch", "-D", "feature/json").Run()
	})
	return clone, improvised, tip
}

func t1311Finish(sha string) string {
	return envelope.Format(&envelope.Message{
		Kind:         envelope.KindFinishReport,
		Target:       "T1311",
		SHA:          sha,
		Verdict:      envelope.VerdictGreen,
		SilentLedger: envelope.SilentLedgerEmpty,
		Payload:      "Landed.",
	})
}

func t1311TreeSurvived(t *testing.T, improvised string) {
	t.Helper()
	if _, err := os.Stat(improvised); err != nil {
		t.Fatalf("improvised worktree was removed: %v", err)
	}
}

func TestT1311ImprovisedWorktreeBriefInstructsCleanup(t *testing.T) {
	clone, improvised, _ := t1311Scratch(t)

	got, note, life := isolateFleetWorkSeat(improvised, "jv-t1311-json", "work", "worker")
	if got != improvised {
		t.Fatalf("workdir = %q, want improvised %q", got, improvised)
	}
	if life["improvised_worktree"] != true {
		t.Fatalf("life missing improvised_worktree: %v", life)
	}
	brief := prependIsolationBrief("Implement the json slice.", note)
	for _, want := range []string{
		"🎯T1311",
		"git worktree remove " + improvised,
		"git branch -d feature/json",
		"Implement the json slice.",
	} {
		if !strings.Contains(brief, want) {
			t.Fatalf("brief missing %q:\n%s", want, brief)
		}
	}
	if strings.Contains(brief, "remove --force") || strings.Contains(brief, "branch -D") {
		t.Fatalf("brief still recommends force delete:\n%s", brief)
	}

	managed, err := worktree.Ensure(clone, "jv-t1311-managed")
	if err != nil {
		t.Fatal(err)
	}
	_, managedNote, managedLife := isolateFleetWorkSeat(clone, "jv-t1311-managed", "work", "worker")
	if managedLife["improvised_worktree"] == true {
		t.Fatal("managed Ensure tree stamped as improvised")
	}
	if strings.Contains(managedNote, "git worktree remove") {
		t.Fatalf("managed isolation note told the worker to self-remove:\n%s", managedNote)
	}
	if !strings.Contains(managedNote, "🎯T1151.1") {
		t.Fatalf("managed isolation note missing integrator instruction:\n%s", managedNote)
	}
	if _, err := os.Stat(managed); err != nil {
		t.Fatalf("managed tree missing: %v", err)
	}
}

func TestT1311ReapRemovesImprovisedWorktree(t *testing.T) {
	_, improvised, tip := t1311Scratch(t)
	name := "jv-t1311-json"
	reg := t1122Registry(t, name, improvised)
	s := &Server{registry: reg}
	s.maybeReapDoneWorkAgent(name, t1311Finish(tip))
	if reg.Def(name) != nil {
		t.Fatal("worker still registered after finished-work reap")
	}
	if _, err := os.Stat(improvised); !os.IsNotExist(err) {
		t.Fatalf("improvised worktree survived auto-reap: %v", err)
	}
}

func TestT1311ReapLeavesUnlandedImprovisedWorktree(t *testing.T) {
	_, improvised, tip := t1311Scratch(t)
	if err := os.WriteFile(filepath.Join(improvised, "json.go"), []byte("package json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t1122GitRun(t, improvised, "add", ".")
	t1122GitRun(t, improvised, "commit", "-q", "-m", "unlanded json")
	name := "jv-t1311-unlanded"
	reg := t1122Registry(t, name, improvised)
	s := &Server{registry: reg}
	s.maybeReapDoneWorkAgent(name, t1311Finish(tip))
	if reg.Def(name) != nil {
		t.Fatal("worker still registered; reap itself must still happen")
	}
	t1311TreeSurvived(t, improvised)
}

func TestT1311ReapLeavesWorkdirStillUsedByAnotherAgent(t *testing.T) {
	_, improvised, tip := t1311Scratch(t)
	name := "jv-t1311-json"
	reg := t1122Registry(t, name, improvised)
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t1311-other", WorkDir: improvised, SessionID: "s-o",
		Purpose: claudia.PurposeWork, Parent: "jevons-po",
		Materialized: true, Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg}
	s.maybeReapDoneWorkAgent(name, t1311Finish(tip))
	if reg.Def(name) != nil {
		t.Fatal("worker still registered after finished-work reap")
	}
	if reg.Def("jv-t1311-other") == nil {
		t.Fatal("sibling agent was reaped")
	}
	t1311TreeSurvived(t, improvised)
}

func TestT1311StopReapRemovesCherryPickedWorktree(t *testing.T) {
	clone, improvised, _ := t1311Scratch(t)
	if err := os.WriteFile(filepath.Join(improvised, "json.go"), []byte("package json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t1122GitRun(t, improvised, "add", ".")
	t1122GitRun(t, improvised, "commit", "-q", "-m", "json slice")
	sha := t1122GitRun(t, improvised, "rev-parse", "HEAD")
	t1122GitRun(t, clone, "cherry-pick", "-x", sha)

	name := "jv-t1311-stop"
	regDir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(regDir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: improvised, SessionID: "s-w",
		Purpose: claudia.PurposeWork, Parent: "jevons-po",
		Materialized: true, Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":        name,
		"disposition": "reap",
		"actor":       "jevons",
	}
	if _, err := s.handleAgentStop(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if reg.Def(name) != nil {
		t.Fatal("worker still registered after stop-as-reap")
	}
	if _, err := os.Stat(improvised); !os.IsNotExist(err) {
		t.Fatalf("cherry-picked worktree survived stop-as-reap: %v", err)
	}
}
