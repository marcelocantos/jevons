// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree_test

import (
	"errors"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/gate"
	"github.com/marcelocantos/jevons/internal/worktree"
)

func t1205SaveTimedRecord(t *testing.T, root, id, commit string, command []string, order int, pristine bool, verdict gate.Verdict) {
	t.Helper()
	store, err := gate.OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 10, 24, 0, order, 0, 0, time.UTC)
	rec := &gate.Record{
		ID: id, Name: "t1205-test", Command: command,
		Started: started, Ended: started.Add(time.Second),
		StatusKnown: true, Verdict: verdict,
		Tree: &gate.TreeProvenance{Repo: root, Commit: commit, Clean: true},
	}
	if pristine {
		rec.Tree.Shared = &gate.SharedTreeState{Repo: root}
	}
	if verdict == gate.VerdictRed {
		rec.ExitStatus = 1
	}
	if err := store.Save(rec); err != nil {
		t.Fatal(err)
	}
}

func TestT1205SelectsNewestEligibleGateRatherThanNewestGreen(t *testing.T) {
	for _, tc := range []struct {
		name, want                    string
		focused, pristine, equivalent bool
		verdict                       gate.Verdict
	}{
		{"newer focused", "older-full", true, true, false, gate.VerdictGreen},
		{"newer focused on equal tree", "older-full", true, true, true, gate.VerdictGreen},
		{"newer full", "newer", false, true, false, gate.VerdictGreen},
		{"newer nonpristine", "older-full", false, false, false, gate.VerdictGreen},
		{"newer failed", "older-full", false, true, false, gate.VerdictRed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := sharedClone(t)
			wt, sha := workerGoPackagesCommit(t, base, "jv-t1205")
			root := t.TempDir()
			full := []string{"go", "test", "./internal/server", "./internal/fleet", "-count=1"}
			t1205SaveTimedRecord(t, root, "older-full", sha, full, 1, true, gate.VerdictGreen)
			if tc.equivalent {
				git(t, wt, "commit", "-q", "--allow-empty", "-m", "same measured tree")
			}
			command := full
			if tc.focused {
				command = []string{"go", "test", "./internal/server", "./internal/fleet", "-run", "TestOne"}
			}
			t1205SaveTimedRecord(t, root, "newer", sha, command, 2, tc.pristine, tc.verdict)
			rec, err := worktree.RequireLandingGate(base, "jv-t1205", root, "")
			if err != nil || rec == nil || rec.ID != tc.want {
				t.Fatalf("selected %+v, want %s: %v", rec, tc.want, err)
			}
		})
	}
}

func TestT1205InsufficientOnlyStillRefuses(t *testing.T) {
	base := sharedClone(t)
	_, sha := workerGoPackagesCommit(t, base, "jv-t1205-insufficient")
	root := t.TempDir()
	t1205SaveTimedRecord(t, root, "missing-package", sha, []string{"go", "test", "./internal/server"}, 1, true, gate.VerdictGreen)
	t1205SaveTimedRecord(t, root, "focused", sha, []string{"go", "test", "./internal/server", "./internal/fleet", "-run", "TestOne"}, 2, true, gate.VerdictGreen)
	rec, err := worktree.RequireLandingGate(base, "jv-t1205-insufficient", root, "")
	if rec != nil || !errors.Is(err, gate.ErrLandingGateCoverage) {
		t.Fatalf("insufficient evidence accepted: %+v %v", rec, err)
	}
}

func TestT1205ExplicitInsufficientCitationDoesNotFallBack(t *testing.T) {
	base := sharedClone(t)
	wt, sha := workerGoPackagesCommit(t, base, "jv-t1205-cited")
	root := t.TempDir()
	t1205SaveTimedRecord(t, root, "full", sha, []string{"go", "test", "./internal/server", "./internal/fleet"}, 1, true, gate.VerdictGreen)
	t1205SaveTimedRecord(t, root, "c17ed001", sha, []string{"go", "test", "./internal/server", "-run", "TestOne"}, 2, true, gate.VerdictGreen)
	git(t, wt, "commit", "-q", "--allow-empty", "-m", "same tree with explicit citation\n\nGATE t1180-test exit=0 GREEN id=c17ed001 out=deadbeefcafe dur=1s")
	rec, err := worktree.RequireLandingGate(base, "jv-t1205-cited", root, "")
	if rec != nil || !errors.Is(err, gate.ErrLandingGateCoverage) {
		t.Fatalf("explicit invalid citation bypassed by another record: %+v %v", rec, err)
	}
}
