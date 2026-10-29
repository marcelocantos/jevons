// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T962: provider=anthropic OMP sidecar seats never read .claude/settings.json,
// so jevons 🎯T955's Claude Code PreToolUse hook does not protect them. The
// equivalent guard lives in the pinned Claudia sidecar (pre-exec in
// execution.ts before runShell). This ratchet checks the wiring and runs the
// sidecar's hermetic branchguard.test.ts — the Bash tool's execute path, not
// a hook file.
package docratchet_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func readClaudiaFile(t *testing.T, moduleDir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(moduleDir, rel))
	if err != nil {
		t.Fatalf("read claudia %s: %v", rel, err)
	}
	return string(b)
}

func TestT962SidecarBranchGuardWiring(t *testing.T) {
	dir := claudiaModuleDir(t)
	execBody := readClaudiaFile(t, dir, "sidecar/execution.ts")
	if !strings.Contains(execBody, "guardBranchCheckout") {
		t.Fatal("sidecar/execution.ts must call guardBranchCheckout before shell (🎯T962)")
	}
	branch := readClaudiaFile(t, dir, "sidecar/branchguard.ts")
	for _, want := range []string{"decideBranchCheckout", "detectBranchCheckout", "guardBranchCheckout", "shared clone"} {
		if !strings.Contains(branch, want) {
			t.Fatalf("sidecar/branchguard.ts missing %q", want)
		}
	}
	coding := readClaudiaFile(t, dir, "sidecar/coding.ts")
	if !strings.Contains(coding, "runShell") {
		t.Fatal("sidecar/coding.ts Bash tool must route through runShell (🎯T962)")
	}
}

func TestT962SidecarBranchGuardHermetic(t *testing.T) {
	if _, err := exec.LookPath("bun"); err != nil {
		t.Skip("bun not on PATH")
	}
	dir := claudiaModuleDir(t)
	cmd := exec.Command("bun", "test", "sidecar/branchguard.test.ts")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bun test sidecar/branchguard.test.ts: %v\n%s", err, out)
	}
	s := string(out)
	if !strings.Contains(s, "0 fail") {
		t.Fatalf("branchguard suite did not pass cleanly:\n%s", s)
	}
}
