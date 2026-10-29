// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

// TestT1255IntegrateHonoursDeclaredTestTags is the doctrine ratchet: the
// touched-package post-merge gate runs go test -tags when the repo declares
// test_tags, instead of an untagged invocation that finds no packages.
func TestT1255IntegrateHonoursDeclaredTestTags(t *testing.T) {
	post := readRepo(t, "internal/worktree/t1146_post_merge_gate.go")
	for _, needle := range []string{
		"T1255",
		"testTags",
		"-tags",
	} {
		if !strings.Contains(post, needle) {
			t.Fatalf("t1146_post_merge_gate.go missing 🎯T1255 seam %q", needle)
		}
	}
	decl := readRepo(t, "internal/worktree/integrate_prereq.go")
	for _, needle := range []string{
		"test_tags",
		"TestTags",
		"T1255",
	} {
		if !strings.Contains(decl, needle) {
			t.Fatalf("integrate_prereq.go missing 🎯T1255 seam %q", needle)
		}
	}
	main := readRepo(t, "cmd/integrate/main.go")
	if !strings.Contains(main, "T1255") {
		t.Fatal("cmd/integrate/main.go must document 🎯T1255")
	}
}
