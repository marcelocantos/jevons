// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// 🎯T1155: bullseye_apply's product path is `bullseye apply` with an explicit
// remove list — the same argv shape MCP forwards. Ephemeral smoke rows must be
// removable without hand-editing bullseye.yaml.
func TestT1155MCPProductEphemeralDelete(t *testing.T) {
	if _, err := exec.LookPath("bullseye"); err != nil {
		t.Skip("bullseye not on PATH")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	repo := t.TempDir()
	gitInit(t, repo)

	open := exec.Command("bullseye", "open", "--cwd", repo, "--location", "in_repo")
	if out, err := open.CombinedOutput(); err != nil {
		t.Fatalf("bullseye open: %v\n%s", err, out)
	}

	track := exec.Command("bullseye", "commit", "--op", "track",
		"--cwd", repo,
		"--name", "T101 ephemeral smoke",
		"--acceptance", "ephemeral smoke",
	)
	out, err := track.CombinedOutput()
	if err != nil {
		t.Fatalf("bullseye commit --op track: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ids:") {
		t.Fatalf("track output missing ids:\n%s", out)
	}

	listBefore := exec.Command("bullseye", "query", "--cwd", repo, "--view", "list", "--filter", "all")
	lb, err := listBefore.CombinedOutput()
	if err != nil {
		t.Fatalf("bullseye query list: %v\n%s", err, lb)
	}
	if !strings.Contains(string(lb), "ephemeral smoke") {
		t.Fatalf("fixture target missing before delete:\n%s", lb)
	}

	fragmentPath := repo + "/remove-fragment.yaml"
	if err := os.WriteFile(fragmentPath, []byte("remove: [T2]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	apply := exec.Command("bullseye", "apply", "--cwd", repo, "-f", fragmentPath)
	ab, err := apply.CombinedOutput()
	if err != nil {
		t.Fatalf("bullseye apply remove: %v\n%s", err, ab)
	}
	if !strings.Contains(string(ab), "Removed") {
		t.Fatalf("apply remove output unexpected:\n%s", ab)
	}

	listAfter := exec.Command("bullseye", "query", "--cwd", repo, "--view", "list", "--filter", "all")
	la, err := listAfter.CombinedOutput()
	if err != nil {
		t.Fatalf("bullseye query list after: %v\n%s", err, la)
	}
	if strings.Contains(string(la), "ephemeral smoke") {
		t.Fatalf("ephemeral row still present after apply remove:\n%s", la)
	}
}
