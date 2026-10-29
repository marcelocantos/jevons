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

// 🎯T1197: a branch that touches only internal/x but violates a docratchet
// ratchet is refused at integrate. The worker package's own tests stay green;
// the refusal names the failing ratchet.

const t1197Ratchet = `package docratchet_test

import (
	"os"
	"strings"
	"testing"
)

func TestT1197ForbiddenToken(t *testing.T) {
	b, err := os.ReadFile("../../internal/x/x.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "FORBIDDEN") {
		t.Fatal("internal/x contains FORBIDDEN")
	}
}
`

func t1197Seed(t *testing.T) string {
	t.Helper()
	base := goModuleSharedClone(t)
	writeFile(t, filepath.Join(base, "internal/x/x.go"), "package x\n\nfunc Value() string { return \"ok\" }\n")
	writeFile(t, filepath.Join(base, "internal/x/x_test.go"), "package x\n\nimport \"testing\"\n\nfunc TestValue(t *testing.T) {\n\tif Value() == \"\" {\n\t\tt.Fatal(\"empty\")\n\t}\n}\n")
	writeFile(t, filepath.Join(base, "scripts/docratchet/ratchet_test.go"), t1197Ratchet)
	git(t, base, "add", ".")
	git(t, base, "commit", "-q", "-m", "seed internal/x and docratchet")
	return base
}

func TestIntegrateRollsBackWhenUntouchedDocratchetRatchetFails(t *testing.T) {
	base := t1197Seed(t)
	wt, err := worktree.Ensure(base, "jv-t1197")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	writeFile(t, filepath.Join(wt, "internal/x/x.go"), "package x\n\nfunc Value() string { return \"FORBIDDEN\" }\n")
	git(t, wt, "add", "internal/x/x.go")
	git(t, wt, "commit", "-q", "-m", "worker: only internal/x")

	writeFile(t, filepath.Join(base, "other.txt"), "trunk moved\n")
	git(t, base, "add", "other.txt")
	git(t, base, "commit", "-q", "-m", "trunk moves elsewhere")
	masterTip := git(t, base, "rev-parse", "master")

	_, err = worktree.Integrate(&worktree.IntegrateArgs{BaseWorkdir: base, AgentName: "jv-t1197"})
	if !errors.Is(err, worktree.ErrPostMergeGate) {
		t.Fatalf("want ErrPostMergeGate, got %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "TestT1197ForbiddenToken") {
		t.Fatalf("refusal must name the failing ratchet, got %v", err)
	}
	if now := git(t, base, "rev-parse", "master"); now != masterTip {
		t.Fatalf("refused landing left master at %s, want rolled back to %s", now, masterTip)
	}
}

func TestIntegrateMergeRunsDocratchetWhenBranchTouchesOnlyInternalX(t *testing.T) {
	base := t1197Seed(t)
	wt, err := worktree.Ensure(base, "jv-ok")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	writeFile(t, filepath.Join(wt, "internal/x/x.go"), "package x\n\nfunc Value() string { return \"ok-2\" }\n")
	git(t, wt, "add", "internal/x/x.go")
	git(t, wt, "commit", "-q", "-m", "worker: only internal/x, ratchet still green")

	writeFile(t, filepath.Join(base, "other.txt"), "trunk moved\n")
	git(t, base, "add", "other.txt")
	git(t, base, "commit", "-q", "-m", "trunk moves elsewhere")

	res, err := worktree.Integrate(&worktree.IntegrateArgs{BaseWorkdir: base, AgentName: "jv-ok"})
	if err != nil {
		t.Fatalf("Integrate: %v", err)
	}
	if res.Merge == "" {
		t.Fatalf("want merge commit, got %+v", res)
	}
	if res.PostMergeGate == nil || !res.PostMergeGate.Verdict.IsGreen() {
		t.Fatalf("want GREEN post-merge gate, got %+v", res.PostMergeGate)
	}
	cmd := strings.Join(res.PostMergeGate.Command, " ")
	if !strings.Contains(cmd, "scripts/docratchet") {
		t.Fatalf("post-merge test command must include scripts/docratchet, got %q", cmd)
	}
	if _, err := os.Stat(filepath.Join(base, "internal/x/x.go")); err != nil {
		t.Fatal(err)
	}
}
