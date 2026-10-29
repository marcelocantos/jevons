// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/worktree"
)

// The gate parser accepts this module's cd-and-test command; the separate
// post-merge runner must execute from that module too, not go test ./nested/...
func TestT1272NestedModulePostMergeRunsFromModule(t *testing.T) {
	base := goModuleSharedClone(t)
	wt, err := worktree.Ensure(base, "jv-nested-merge")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(wt, "nested/go.mod"), "module example.com/nested\n\ngo 1.22\n")
	writeFile(t, filepath.Join(wt, "nested/p/p.go"), "package p\nfunc P() {}\n")
	writeFile(t, filepath.Join(wt, "nested/p/p_test.go"), "package p\nimport \"testing\"\nfunc TestP(t *testing.T) { P() }\n")
	git(t, wt, "add", "nested")
	git(t, wt, "commit", "-q", "-m", "add nested module")
	writeFile(t, filepath.Join(base, "trunk.txt"), "moved\n")
	git(t, base, "add", "trunk.txt")
	git(t, base, "commit", "-q", "-m", "move trunk")
	res, err := worktree.Integrate(&worktree.IntegrateArgs{BaseWorkdir: base, AgentName: "jv-nested-merge", GateStoreRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("nested post-merge refused: %v", err)
	}
	if res.PostMergeGate == nil || !res.PostMergeGate.Verdict.IsGreen() {
		t.Fatalf("post-merge not green: %+v", res.PostMergeGate)
	}
	cmd := strings.Join(res.PostMergeGate.Command, " ")
	if !strings.Contains(cmd, "cd 'nested' && GOWORK=off 'go' 'test'") || strings.Contains(cmd, "./nested/...") {
		t.Fatalf("nested module not tested from module: %q", cmd)
	}
}

func TestT1272RootModulePostMergeKeepsRootCommand(t *testing.T) {
	base := goModuleSharedClone(t)
	wt, err := worktree.Ensure(base, "jv-root-merge")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(wt, "p1/p1_test.go"), "package p1\nimport \"testing\"\nfunc TestP(t *testing.T) { P() }\n")
	git(t, wt, "add", "p1/p1_test.go")
	git(t, wt, "commit", "-q", "-m", "add root test")
	writeFile(t, filepath.Join(base, "trunk.txt"), "moved\n")
	git(t, base, "add", "trunk.txt")
	git(t, base, "commit", "-q", "-m", "move trunk")
	res, err := worktree.Integrate(&worktree.IntegrateArgs{BaseWorkdir: base, AgentName: "jv-root-merge", GateStoreRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	cmd := res.PostMergeGate.Command
	if len(cmd) < 3 || cmd[0] != "go" || cmd[1] != "test" || !strings.Contains(strings.Join(cmd, " "), "./p1/...") {
		t.Fatalf("root command changed: %q", cmd)
	}
}
