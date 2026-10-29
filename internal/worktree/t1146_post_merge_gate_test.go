// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/worktree"
)

// Oracles for 🎯T1146 post-merge gate: two independently buildable branches
// whose merge introduces an import cycle are refused and the shared branch
// is rolled back.

func goModuleSharedClone(t *testing.T) string {
	t.Helper()
	base := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, base, "init", "-q", "-b", "master")
	git(t, base, "config", "user.email", "t@example.com")
	git(t, base, "config", "user.name", "t")
	writeFile(t, filepath.Join(base, "go.mod"), "module example.com/integratetest\n\ngo 1.22\n")
	writeFile(t, filepath.Join(base, "p1/p1.go"), "package p1\n\nfunc P() {}\n")
	writeFile(t, filepath.Join(base, "p2/p2.go"), "package p2\n\nfunc Q() {}\n")
	git(t, base, "add", ".")
	git(t, base, "commit", "-q", "-m", "seed go module")
	return base
}

func TestIntegrateRollsBackMergeThatIntroducesImportCycle(t *testing.T) {
	base := goModuleSharedClone(t)
	wt, err := worktree.Ensure(base, "jv-cycle")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	writeFile(t, filepath.Join(wt, "p2/p2.go"), "package p2\n\nimport \"example.com/integratetest/p1\"\n\nfunc Q() { p1.P() }\n")
	git(t, wt, "add", "p2/p2.go")
	git(t, wt, "commit", "-q", "-m", "worker: p2 imports p1")

	writeFile(t, filepath.Join(base, "p1/p1.go"), "package p1\n\nimport \"example.com/integratetest/p2\"\n\nfunc P() { p2.Q() }\n")
	git(t, base, "commit", "-q", "-am", "master: p1 imports p2")
	masterTip := git(t, base, "rev-parse", "master")

	_, err = worktree.Integrate(&worktree.IntegrateArgs{BaseWorkdir: base, AgentName: "jv-cycle"})
	if !errors.Is(err, worktree.ErrPostMergeGate) {
		t.Fatalf("want ErrPostMergeGate, got %v", err)
	}
	if now := git(t, base, "rev-parse", "master"); now != masterTip {
		t.Fatalf("refused landing left master at %s, want rolled back to %s", now, masterTip)
	}
	// The rolled-back merge commit still exists; the error must not leave
	// a reader free to cite it as landed.
	if !strings.Contains(err.Error(), "NOT LANDED: master stays at "+masterTip[:8]) {
		t.Fatalf("rollback error does not say NOT LANDED with the restored tip: %v", err)
	}
}

func TestIntegrateMergePassesPostMergeGateWhenCombinedTreeBuilds(t *testing.T) {
	base := goModuleSharedClone(t)
	wt, err := worktree.Ensure(base, "jv-ok")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	writeFile(t, filepath.Join(wt, "p2/p2.go"), "package p2\n\nimport \"example.com/integratetest/p1\"\n\nfunc Q() { p1.P() }\n")
	writeFile(t, filepath.Join(wt, "p2/p2_test.go"), "package p2\n\nimport \"testing\"\n\nfunc TestQ(t *testing.T) { Q() }\n")
	git(t, wt, "add", "p2")
	git(t, wt, "commit", "-q", "-m", "worker: p2 imports p1")

	writeFile(t, filepath.Join(base, "other.txt"), "master moved\n")
	git(t, base, "add", "other.txt")
	git(t, base, "commit", "-q", "-m", "master moves elsewhere")

	res, err := worktree.Integrate(&worktree.IntegrateArgs{BaseWorkdir: base, AgentName: "jv-ok"})
	if err != nil {
		t.Fatalf("Integrate: %v", err)
	}
	if res.Merge == "" {
		t.Fatalf("want merge commit, got %+v", res)
	}
	if res.PostMergeGate == nil || !res.PostMergeGate.Verdict.IsGreen() {
		t.Fatalf("want GREEN post-merge gate record, got %+v", res.PostMergeGate)
	}
}

// Trunk can gain tests after a branch was cut; each side is green alone but the
// merge result fails those tests — integrate must refuse (🎯T1191).
func TestIntegrateRollsBackMergeWhenTrunkTestFailsOnCombinedTree(t *testing.T) {
	base := goModuleSharedClone(t)
	writeFile(t, filepath.Join(base, "lib/lib.go"), "package lib\n\nfunc Value() int { return 1 }\n")
	git(t, base, "add", "lib/lib.go")
	git(t, base, "commit", "-q", "-m", "seed lib")

	wt, err := worktree.Ensure(base, "jv-stale")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	writeFile(t, filepath.Join(wt, "lib/lib.go"), "package lib\n\nfunc Value() int { return 2 }\n")
	writeFile(t, filepath.Join(wt, "lib/lib_test.go"), "package lib\n\nimport \"testing\"\n\nfunc TestValueIsTwo(t *testing.T) {\n\tif Value() != 2 {\n\t\tt.Fatalf(\"got %d\", Value())\n\t}\n}\n")
	git(t, wt, "add", "lib")
	git(t, wt, "commit", "-q", "-m", "worker: Value returns 2")

	writeFile(t, filepath.Join(base, "lib/value_one_test.go"), "package lib\n\nimport \"testing\"\n\nfunc TestValueIsOne(t *testing.T) {\n\tif Value() != 1 {\n\t\tt.Fatalf(\"got %d, want 1\", Value())\n\t}\n}\n")
	git(t, base, "add", "lib/value_one_test.go")
	git(t, base, "commit", "-q", "-m", "trunk: expect Value 1")
	masterTip := git(t, base, "rev-parse", "master")

	_, err = worktree.Integrate(&worktree.IntegrateArgs{BaseWorkdir: base, AgentName: "jv-stale"})
	if !errors.Is(err, worktree.ErrPostMergeGate) {
		t.Fatalf("want ErrPostMergeGate, got %v", err)
	}
	if now := git(t, base, "rev-parse", "master"); now != masterTip {
		t.Fatalf("refused landing left master at %s, want rolled back to %s", now, masterTip)
	}
}

func TestIntegrateFastForwardSkipsPostMergeGate(t *testing.T) {
	base := goModuleSharedClone(t)
	wt, err := worktree.Ensure(base, "jv-ff")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	writeFile(t, filepath.Join(wt, "p2/p2.go"), "package p2\n\nfunc Q() int { return 1 }\n")
	git(t, wt, "add", "p2/p2.go")
	git(t, wt, "commit", "-q", "-m", "worker only")

	res, err := worktree.Integrate(&worktree.IntegrateArgs{BaseWorkdir: base, AgentName: "jv-ff"})
	if err != nil {
		t.Fatalf("Integrate: %v", err)
	}
	if res.Merge != "" {
		t.Fatalf("want fast-forward, got merge %s", res.Merge)
	}
	if res.PostMergeGate != nil {
		t.Fatalf("fast-forward should skip post-merge gate, got %+v", res.PostMergeGate)
	}
}
