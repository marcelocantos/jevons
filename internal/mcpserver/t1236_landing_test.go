// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package mcpserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/gate"
	"github.com/marcelocantos/jevons/internal/worktreereap"
)

// The integration fast-forward was rolled back, but the merge object remains
// reachable in the worker's object store and at the worker worktree HEAD.
func TestT1236RolledBackLandingIsFlaggedAtNotifyAndVetoedAtReap(t *testing.T) {
	shared := seededRepoNamed(t, "shared")
	worker := filepath.Join(t.TempDir(), "worker")
	gitRun(t, shared, "worktree", "add", "-b", "worker", worker)
	// The test tree is disposable but must be marked for cleanup if the test dies.
	if err := worktreereap.Mark(&worktreereap.MarkArgs{Worktree: worker, Note: t.Name()}); err != nil {
		t.Fatal(err)
	}
	ancestor := gitHead(t, shared)
	if err := os.WriteFile(filepath.Join(worker, "change"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, worker, "add", "change")
	gitRun(t, worker, "commit", "-m", "change")
	// Make an actual two-parent integration object, fast-forward to it, then
	// roll back the shared tip while retaining the merge on the worker branch.
	gitRun(t, shared, "-c", "user.name=Test", "-c", "user.email=test@example.com", "merge", "--no-ff", "worker", "-m", "integrate")
	merge := gitHead(t, shared)
	gitRun(t, worker, "merge", "--ff-only", merge)
	gitRun(t, shared, "reset", "--hard", ancestor)
	if err := exec.Command("git", "-C", worker, "cat-file", "-e", merge+"^{commit}").Run(); err != nil {
		t.Fatal("merge object missing", err)
	}
	report := "```jevons\njevons: kind finish-report\njevons: target T1236\njevons: oracle sha=" + merge + "\njevons: silent-ledger none\n```"
	flags := FalseGreenFlagsForReport(report, worker)
	if !hasKind(flags, gate.FlagLandingUnreachable) || !strings.Contains(gate.BannerFor("worker", flags), "shared checkout") {
		t.Fatalf("missing landing banner: %+v", flags)
	}
	reg := regWithWorkerDir(t, worker)
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, "worker", report, nil)
	if ok || reason != "false_green_landing_unreachable" {
		t.Fatalf("dangling merge reaped: %v %s", ok, reason)
	}
	// A PO's prose landing assertion is bannered, even without an envelope.
	if flags := FalseGreenFlagsForReport("integrated at merge "+merge, worker); !hasKind(flags, gate.FlagLandingUnreachable) {
		t.Fatalf("PO claim missed: %+v", flags)
	}
	control := strings.Replace(report, merge, ancestor, 1)
	if flags := FalseGreenFlagsForReport(control, worker); hasKind(flags, gate.FlagLandingUnreachable) {
		t.Fatalf("ancestor flagged: %+v", flags)
	}
	if ok, reason := ShouldAutoReapDoneWorkAgent(reg, "worker", control, nil); !ok {
		t.Fatalf("ancestor not reapable: %s", reason)
	}
}
