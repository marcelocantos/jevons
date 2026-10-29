// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Hermetic oracle: killPrivateClaudiaTmux stops the isolate's claudia-anchor
// server before the temp tree is removed (🎯T780 / 🎯T846).
func TestT780KillPrivateClaudiaTmuxStopsAnchorServer(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	stateDir, err := os.MkdirTemp("", "jevons-journey-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(stateDir) })

	claudiaDir := filepath.Join(stateDir, "claudia")
	if err := os.MkdirAll(claudiaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(claudiaDir, "tmux.sock")

	start := exec.Command("tmux", "-S", sock, "new-session", "-d", "-s", "claudia-anchor")
	if out, err := start.CombinedOutput(); err != nil {
		t.Fatalf("start tmux anchor: %v\n%s", err, out)
	}

	killPrivateClaudiaTmux(stateDir)

	check := exec.Command("tmux", "-S", sock, "has-session", "-t", "claudia-anchor")
	if err := check.Run(); err == nil {
		t.Fatal("claudia-anchor session still alive after killPrivateClaudiaTmux")
	}
}
