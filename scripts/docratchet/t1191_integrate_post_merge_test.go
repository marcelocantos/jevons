// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

// TestT1191IntegratePostMergeTestsTouchedPackages is the doctrine ratchet:
// merge landings run go test on branch-touched packages at the merge result.
func TestT1191IntegratePostMergeTestsTouchedPackages(t *testing.T) {
	post := readRepo(t, "internal/worktree/t1146_post_merge_gate.go")
	for _, needle := range []string{
		"T1191",
		"GoTestCount1Argv",
		"integrate-post-merge-test",
	} {
		if !strings.Contains(post, needle) {
			t.Fatalf("t1146_post_merge_gate.go missing 🎯T1191 seam %q", needle)
		}
	}
	main := readRepo(t, "cmd/integrate/main.go")
	if !strings.Contains(main, "T1191") {
		t.Fatal("cmd/integrate/main.go must document 🎯T1191")
	}
}
