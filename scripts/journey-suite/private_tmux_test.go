// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 🎯T780 / T846: a journey isolate's private claudia-anchor tmux server must
// die with the isolate. The live leak was one anchor per interrupted or
// exitNow-terminated run, held under ppid 1 for hours.

func TestKillPrivateClaudiaTmuxStopsAnchor(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable")
	}
	root := t.TempDir()
	sockDir := filepath.Join(root, "claudia")
	if err := os.MkdirAll(sockDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(sockDir, "tmux.sock")
	// Start a private server the same way claudia's legacy path does.
	cmd := exec.Command("tmux", "-S", sock, "new-session", "-d", "-s", "claudia-anchor")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("start private tmux: %v\n%s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "-S", sock, "kill-server").Run() })

	// Confirm it is live before the kill.
	if err := exec.Command("tmux", "-S", sock, "has-session", "-t", "claudia-anchor").Run(); err != nil {
		t.Fatalf("anchor not live before kill: %v", err)
	}

	killPrivateClaudiaTmux(root)

	deadline := time.Now().Add(5 * time.Second)
	for {
		err := exec.Command("tmux", "-S", sock, "has-session", "-t", "claudia-anchor").Run()
		if err != nil {
			return // gone — pass
		}
		if time.Now().After(deadline) {
			t.Fatal("private claudia-anchor still answers after killPrivateClaudiaTmux")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// killPrivateClaudiaTmux must live inside stop(), not only a defer: exitNow
// and fatal call os.Exit and skip every defer (🎯T379 / T780).
func TestStopArmsPrivateTmuxKill(t *testing.T) {
	path := os.Getenv("JOURNEY_MAIN_SRC")
	if path == "" {
		path = "main.go"
	}
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := string(src)

	stopAt := strings.Index(text, "stop := func() {")
	if stopAt < 0 {
		t.Fatal("no stop := func() in main.go")
	}
	// Bound the stop body roughly: next top-level registration after stop.
	rest := text[stopAt:]
	end := strings.Index(rest, "cleanups.Add(stop)")
	if end < 0 {
		t.Fatal("cleanups.Add(stop) missing — stop is no longer registered")
	}
	body := rest[:end]
	if !strings.Contains(body, "killPrivateClaudiaTmux(stateDir)") {
		t.Error("stop() does not call killPrivateClaudiaTmux — exitNow paths will orphan the claudia-anchor (🎯T780)")
	}
}
