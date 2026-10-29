// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestT955BranchGuardRatchet refuses a landing path that drops the shared-clone
// branch-checkout guard (🎯T955 acceptance: docratchet ratchets the guard).
func TestT955BranchGuardRatchet(t *testing.T) {
	guard := readRepo(t, "internal/treeguard/guard.go")
	for _, needle := range []string{
		"DecideBranchCheckout",
		"branch-switch-in-shared-clone",
		"T955",
	} {
		if !strings.Contains(guard, needle) {
			t.Fatalf("internal/treeguard/guard.go missing 🎯T955 seam %q", needle)
		}
	}
	hook := readRepo(t, "internal/treeguard/hook.go")
	if !strings.Contains(hook, "decideBranchCheckout") {
		t.Fatal("internal/treeguard/hook.go must wire decideBranchCheckout (🎯T955)")
	}
	if !strings.Contains(readRepo(t, "cmd/treeguard/main.go"), "LogBranchCheckoutDenial") {
		t.Fatal("cmd/treeguard/main.go must log branch-checkout denials to eventlog (🎯T955)")
	}
	integrate := readRepo(t, "internal/worktree/integrate.go")
	for _, needle := range []string{
		"CheckIntegrationBranch",
		"assertSharedCheckoutUnchanged",
		"T955",
	} {
		if !strings.Contains(integrate, needle) {
			t.Fatalf("internal/worktree/integrate.go missing 🎯T955 seam %q", needle)
		}
	}
	if !strings.Contains(readRepo(t, "internal/worktree/worktree.go"), "RecordIntegrationBranch") {
		t.Fatal("internal/worktree/worktree.go must record integration branch at Ensure (🎯T955)")
	}
	if !strings.Contains(readRepo(t, "cmd/fleetgit/main.go"), "RefuseBranchCheckoutInSharedClone") {
		t.Fatal("cmd/fleetgit/main.go must refuse branch checkout in shared clone (🎯T955 / 🎯T962)")
	}
	cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/marcelocantos/claudia")
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list claudia: %v", err)
	}
	claudiaDir := strings.TrimSpace(string(out))
	execPath, err := os.ReadFile(filepath.Join(claudiaDir, "sidecar", "execution.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(execPath), "guardBranchCheckout") {
		t.Fatal("pinned claudia sidecar must guard branch checkout in runShell (🎯T962)")
	}
}
