// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/testhygiene"
)

const childTmuxFatal = "tmux-fatal"

// 🎯T846: a journey isolate must kill its claudia-anchor tmux server and
// remove its state dir on every exit path, not only the happy path.
func TestT846JourneyIsolateTeardownRemovesAnchor(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not on PATH")
	}
	rootMarker := filepath.Join(t.TempDir(), "root")
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(),
		childEnv+"="+childTmuxFatal,
		"JOURNEY_T846_ROOT_MARKER="+rootMarker,
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("child must exit non-zero: %v\n%s", err, out)
	}
	rootBytes, err := os.ReadFile(rootMarker)
	if err != nil {
		t.Fatalf("child did not record isolate root: %v\n%s", err, out)
	}
	root := strings.TrimSpace(string(rootBytes))
	sock := filepath.Join(root, "claudia", "tmux.sock")

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(root); os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("isolate root %s still present after teardown", root)
		}
		time.Sleep(50 * time.Millisecond)
	}
	anchors, err := testhygiene.CensusJourneyAnchors()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range anchors {
		if a.Socket == sock || a.Root == root {
			t.Fatalf("tmux anchor still running: %+v", a)
		}
	}
}

func startClaudiaAnchorTmux(stateRoot string) error {
	sockDir := filepath.Join(stateRoot, "claudia")
	if err := os.MkdirAll(sockDir, 0o700); err != nil {
		return err
	}
	sock := filepath.Join(sockDir, "tmux.sock")
	out, err := exec.Command("tmux", "-S", sock, "new-session", "-d", "-s", "claudia-anchor").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func runT846TmuxFatalChild() {
	stateDir, err := os.MkdirTemp("", "jevons-journey-*")
	if err != nil {
		fatal(err)
	}
	marker := os.Getenv("JOURNEY_T846_ROOT_MARKER")
	if marker != "" {
		_ = os.WriteFile(marker, []byte(stateDir), 0o600)
	}
	if err := startClaudiaAnchorTmux(stateDir); err != nil {
		fatal(err)
	}
	cleanups.Add(func() { os.RemoveAll(stateDir) })
	cleanups.Add(func() { killPrivateClaudiaTmux(stateDir) })
	fatal(errors.New("simulated isolate failure"))
}

func TestKillPrivateClaudiaTmuxStopsServer(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not on PATH")
	}
	root, err := os.MkdirTemp("", "jevons-journey-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	if err := startClaudiaAnchorTmux(root); err != nil {
		t.Fatal(err)
	}
	killPrivateClaudiaTmux(root)
	anchors, err := testhygiene.CensusJourneyAnchors()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range anchors {
		if strings.HasPrefix(a.Root, root) {
			t.Fatalf("server still listed after kill: %+v", a)
		}
	}
}

func TestT846SweepStaleJourneyDirHermetic(t *testing.T) {
	parent, err := os.MkdirTemp("", "jevons-sweep-parent-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(parent) })
	root := filepath.Join(parent, "jevons-journey-sweep")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "jevonsd.log"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	n, err := testhygiene.SweepStaleJourneyDirs(parent)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("swept %d dirs, want 1", n)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("stale dir still present")
	}
}

