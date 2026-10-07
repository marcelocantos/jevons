// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/gate"
)

// 🎯T1027 daemon half: a work agent whose finish report cites a gate id that
// resolves to another repository's run is caught on both paths the daemon
// reads reports on — the notify banner and the 🎯T165 reap — and
// jevons_gate_show answers the scope question for a caller that names its
// own workdir.
//
// The specimen is the 2026-10-07 incident with its roles made explicit:
// 8c015098 is arrai-t41-slowpath-corpus, measured in github.com/arr-ai/arrai;
// the worker citing it sits in a jevons worktree. The stored record here is
// GREEN on purpose. The real one was RED and the existing checker flagged
// that; the foreign origin is what nothing flagged, and a GREEN foreign gate
// is the shape that would have been accepted as evidence.

func gitHead(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD in %s: %v", dir, err)
	}
	return strings.TrimSpace(string(out))
}

// t1027Fixture seeds a hermetic store with a GREEN record measured in
// `arrai`, and returns the arrai and jevons repos and the record.
func t1027Fixture(t *testing.T) (arrai, jevons string, rec *gate.Record) {
	t.Helper()
	root := t.TempDir()
	t.Setenv(gate.StoreDirEnv, root)
	store, err := gate.OpenStore(root)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	// Two repos with different histories. seededGitWorktree seeds identical
	// content under identical author and second-resolution timestamps, so two
	// of them can share a commit sha — which is exactly the collision this
	// fixture must not have.
	arrai = seededRepoNamed(t, "arrai")
	jevons = seededRepoNamed(t, "jevons")
	if gitHead(t, arrai) == gitHead(t, jevons) {
		t.Fatal("fixture repos share a commit; the scope check would be vacuous")
	}
	rec = &gate.Record{
		ID:          "8c015098",
		Name:        "arrai-t41-slowpath-corpus",
		Command:     []string{"go", "test", "-race", "-tags", "timingsensitive,slowpath", "./..."},
		ExitStatus:  0,
		StatusKnown: true,
		Verdict:     gate.VerdictGreen,
		Started:     time.Now().Add(-time.Minute),
		Ended:       time.Now(),
		Tree: &gate.TreeProvenance{
			Repo:   arrai,
			Commit: gitHead(t, arrai),
			Clean:  true,
			Shared: &gate.SharedTreeState{Repo: arrai},
		},
	}
	if err := store.Save(rec); err != nil {
		t.Fatalf("Save: %v", err)
	}
	return arrai, jevons, rec
}

// seededRepoNamed makes a clean one-commit repo whose commit is distinct per
// name: the file name and content carry the name.
func seededRepoNamed(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	gitInit(t, dir)
	gitRun(t, dir, "config", "user.email", name+"@example.com")
	gitRun(t, dir, "config", "user.name", name)
	if err := os.WriteFile(filepath.Join(dir, name+".go"), []byte("package "+name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", name+".go")
	gitRun(t, dir, "commit", "-m", "seed "+name)
	return dir
}

func hasKind(flags []gate.Flag, k gate.FlagKind) bool {
	for _, f := range flags {
		if f.Kind == k {
			return true
		}
	}
	return false
}

// The notify-path check: the report, read as the jevons worker's, is flagged
// foreign; the same report read as the arrai worker's is not.
func TestT1027NotifyPathFlagsACrossRepoGateCitation(t *testing.T) {
	arrai, jevons, rec := t1027Fixture(t)
	report := "🎯T1024 is implemented and committed. Done.\n\n    " + rec.Attestation() + "\n"

	flags := FalseGreenFlagsForReport(report, jevons)
	if !hasKind(flags, gate.FlagForeignRepoGate) {
		t.Fatalf("jevons worker citing an arrai gate was not flagged: %v", falseGreenKinds(flags))
	}
	if flags := FalseGreenFlagsForReport(report, arrai); len(flags) != 0 {
		t.Fatalf("arrai worker citing its own gate was flagged: %v", falseGreenKinds(flags))
	}
	// Without a workdir the check cannot scope and must stay silent, so the
	// legacy FalseGreenFlags path is unchanged for a GREEN record.
	if flags := FalseGreenFlags(report); len(flags) != 0 {
		t.Fatalf("unscoped check flagged a GREEN record: %v", falseGreenKinds(flags))
	}
	banner := gate.BannerFor("jv-t1024-reap-waiting-worker-scout", flags)
	if !strings.Contains(banner, "Report from jv-t1024-reap-waiting-worker-scout.") {
		t.Fatalf("banner does not name the author:\n%s", banner)
	}
	if !strings.Contains(banner, arrai) {
		t.Fatalf("banner does not name the repo the gate measured:\n%s", banner)
	}
}

// The reap path: a finished-work report resting on a foreign gate is never
// reaped finished_work; the veto names the flag.
func TestT1027ReapPathRefusesACrossRepoGateCitation(t *testing.T) {
	arrai, jevons, rec := t1027Fixture(t)
	report := "Implemented the fix. All tests pass. Done.\n\n    " + rec.Attestation() + "\n"
	if !LooksLikeFinishedWorkReport(report) {
		t.Fatal("control must look like finished_work before the T1027 veto")
	}
	reg := regWithWorkerDir(t, jevons)
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, "worker", report, func(string) bool { return false })
	if ok {
		t.Fatal("a report resting on another repo's gate was reaped finished_work")
	}
	if reason != "false_green_"+string(gate.FlagForeignRepoGate) {
		t.Fatalf("reason = %q, want false_green_foreign_repo_gate", reason)
	}

	// Control: the worker that actually ran the gate reaps on it.
	reg = regWithWorkerDir(t, arrai)
	ok, reason = ShouldAutoReapDoneWorkAgent(reg, "worker", report, func(string) bool { return false })
	if !ok {
		t.Fatalf("own-repo control did not reap (reason %s)", reason)
	}
}

// jevons_gate_show with workdir: the caller learns before citing.
func TestT1027GateShowAnswersScopeForTheCaller(t *testing.T) {
	arrai, jevons, rec := t1027Fixture(t)
	s := New(t.TempDir(), nil, nil)
	show := func(workdir string) string {
		t.Helper()
		req := mcp.CallToolRequest{}
		args := map[string]any{"id": rec.ID}
		if workdir != "" {
			args["workdir"] = workdir
		}
		req.Params.Arguments = args
		res, err := s.handleGateShow(context.Background(), req)
		if err != nil || res == nil || res.IsError {
			t.Fatalf("handleGateShow: %v %+v", err, res)
		}
		return resultText(t, res)
	}
	foreign := show(jevons)
	if !strings.Contains(foreign, "scope: FOREIGN") || !strings.Contains(foreign, `"verdict":"foreign"`) {
		t.Fatalf("foreign scope not reported:\n%s", foreign)
	}
	if !strings.Contains(foreign, arrai) || !strings.Contains(foreign, jevons) {
		t.Fatalf("scope line does not name both repos:\n%s", foreign)
	}
	if own := show(arrai); !strings.Contains(own, "scope: own") {
		t.Fatalf("own scope not reported:\n%s", own)
	}
	// Legacy call shape is unchanged: no workdir, no scope line.
	if plain := show(""); strings.Contains(plain, "scope:") {
		t.Fatalf("scope line appeared without a workdir:\n%s", plain)
	}
}
