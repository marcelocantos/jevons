// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

// TestT1197IntegratePostMergeAlwaysRunsDocratchet is the doctrine ratchet:
// merge landings add ./scripts/docratchet/ to the post-merge test set.
func TestT1197IntegratePostMergeAlwaysRunsDocratchet(t *testing.T) {
	post := readRepo(t, "internal/worktree/t1146_post_merge_gate.go")
	for _, needle := range []string{
		"T1197",
		"scripts/docratchet",
		"postMergeAlwaysOnPackages",
		"failingTestNames",
	} {
		if !strings.Contains(post, needle) {
			t.Fatalf("t1146_post_merge_gate.go missing 🎯T1197 seam %q", needle)
		}
	}
	main := readRepo(t, "cmd/integrate/main.go")
	if !strings.Contains(main, "T1197") {
		t.Fatal("cmd/integrate/main.go must document 🎯T1197")
	}
}
