// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T863: the thin Lean oracle must build Invariants with zero sorry.
package writconf_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitRepoRoot(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skip("not a git work tree")
	}
	return strings.TrimSpace(string(out))
}

func TestT863LeanOracleCheck(t *testing.T) {
	root := gitRepoRoot(t)
	if _, err := exec.LookPath("lean"); err != nil {
		t.Skip("lean not on PATH")
	}
	if _, err := exec.LookPath("lake"); err != nil {
		t.Skip("lake not on PATH")
	}
	script := filepath.Join(root, "scripts", "lean-oracle.sh")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("missing %s: %v", script, err)
	}
	cmd := exec.Command(script, "check")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("lean-oracle check: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "sorry_count=0") {
		t.Fatalf("expected sorry_count=0 in output:\n%s", out)
	}
}
