// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleetgit_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/commitattrib"
	"github.com/marcelocantos/jevons/internal/treeguard"
	"github.com/marcelocantos/jevons/internal/worktree"
)

func TestRefuseBranchCheckoutInSharedClone(t *testing.T) {
	base := t.TempDir()
	git(t, base, "init", "-q", "-b", "steer")
	git(t, base, "config", "user.email", "t@example.com")
	git(t, base, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(base, "f"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, base, "add", "f")
	git(t, base, "commit", "-q", "-m", "init")
	git(t, base, "branch", "master")

	wt, err := worktree.Ensure(base, "jv-t955-seat")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(commitattrib.ActorEnv, "jv-t955-seat")

	decision, ok := treeguard.RefuseBranchCheckoutInSharedClone(wt, base, "git -C "+base+" checkout master", true)
	if !ok || decision.Verdict != treeguard.Deny {
		t.Fatalf("decision %+v ok=%v", decision, ok)
	}
	for _, want := range []string{"steer", "master", "T955"} {
		if !strings.Contains(decision.Message, want) {
			t.Errorf("message %q missing %q", decision.Message, want)
		}
	}
}
