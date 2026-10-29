// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

// TestT1249IntegrateRunsRepoDeclaredPrereq is the doctrine ratchet: merge
// landings run a repo-declared prerequisite in the pristine checkout before
// go build / touched-package tests.
func TestT1249IntegrateRunsRepoDeclaredPrereq(t *testing.T) {
	post := readRepo(t, "internal/worktree/t1146_post_merge_gate.go")
	for _, needle := range []string{
		"T1249",
		"loadIntegratePrereq",
		"Prepare",
	} {
		if !strings.Contains(post, needle) {
			t.Fatalf("t1146_post_merge_gate.go missing 🎯T1249 seam %q", needle)
		}
	}
	decl := readRepo(t, "internal/worktree/integrate_prereq.go")
	for _, needle := range []string{
		`.jevons-integrate-prereq.yaml`,
		"submodules",
		"ErrIntegratePrereq",
	} {
		if !strings.Contains(decl, needle) {
			t.Fatalf("integrate_prereq.go missing 🎯T1249 seam %q", needle)
		}
	}
	main := readRepo(t, "cmd/integrate/main.go")
	if !strings.Contains(main, "T1249") {
		t.Fatal("cmd/integrate/main.go must document 🎯T1249")
	}
}
