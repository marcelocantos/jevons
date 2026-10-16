// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// productionClaudiaTmuxSocket mirrors claudia/internal/tmuxagent.SocketPath's
// fallback when CLAUDIA_TMUX_SOCKET is unset: $XDG_STATE_HOME/claudia/tmux.sock
// (with ~/.local/state as the XDG fallback). That is the live fleet socket an
// unguarded test must never address.
func productionClaudiaTmuxSocket() string {
	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		stateHome = filepath.Join(os.Getenv("HOME"), ".local", "state")
	}
	return filepath.Join(stateHome, "claudia", "tmux.sock")
}

// TestT755TestMainPinsTmuxSocketOffTheLiveFleet ratchets 🎯T755.
//
// Pre-T740, Server.paneIO defaulted to the live fleet tmux socket, so an
// unguarded test that reached handleAgentList / SweepOrphanPanes could call
// tmux kill-pane against the owner's panes (exit-137 incident). The pane
// sweep is gone (7b5e727b), but claudia's Registry.Remove still reaches
// tmuxagent via CLAUDIA_TMUX_SOCKET. TestMain must pin that env at a private
// path for the whole package. Leaving it unset (or pointing it at the live
// default) goes RED.
func TestT755TestMainPinsTmuxSocketOffTheLiveFleet(t *testing.T) {
	got := os.Getenv("CLAUDIA_TMUX_SOCKET")
	if got == "" {
		t.Fatal("CLAUDIA_TMUX_SOCKET is unset: TestMain no longer pins mcpserver tests off the live fleet socket (🎯T755)")
	}
	live := productionClaudiaTmuxSocket()
	if got == live {
		t.Fatalf("CLAUDIA_TMUX_SOCKET=%q is the live fleet socket; unguarded tests would reach production panes", got)
	}
	// Resolve both so a relative or symlink trick cannot look different
	// while still landing on the live path.
	gotAbs, err := filepath.Abs(got)
	if err != nil {
		t.Fatalf("abs test socket: %v", err)
	}
	liveAbs, err := filepath.Abs(live)
	if err != nil {
		t.Fatalf("abs live socket: %v", err)
	}
	if gotAbs == liveAbs {
		t.Fatalf("CLAUDIA_TMUX_SOCKET resolves to the live fleet socket %q", liveAbs)
	}
	// The private path must sit under the suite temp dir TestMain created,
	// not under the owner's state home (where the live socket lives).
	liveDir := filepath.Dir(liveAbs)
	if liveDir != "." && (gotAbs == liveDir || strings.HasPrefix(gotAbs, liveDir+string(os.PathSeparator))) {
		t.Fatalf("CLAUDIA_TMUX_SOCKET=%q is under the live state dir %q", gotAbs, liveDir)
	}
	if !strings.HasSuffix(gotAbs, "tmux.sock") {
		t.Fatalf("CLAUDIA_TMUX_SOCKET=%q does not end in tmux.sock; TestMain shape drifted", gotAbs)
	}
}

// TestT755NoProductionKillPaneDefault ratchets that jevons production code
// no longer carries a paneIO / SweepOrphanPanes path whose default is the
// live fleet socket. The T755 hazard was that default; with the sweep gone,
// the only remaining isolation seam is TestMain (covered above). This scan
// fails closed if the deleted production callers return.
func TestT755NoProductionKillPaneDefault(t *testing.T) {
	root := repoRootForT755(t)
	forbidden := []string{
		"SweepOrphanPanes",
		"paneIO",
		"AgentTmuxSocket",
		"NormalizeAgentTmuxEnv",
		"kill-pane",
		"list-panes",
	}
	var hits []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "node_modules", "ui", "_scratchpad", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		// This file names the forbidden tokens in comments/strings; skip it.
		if strings.HasSuffix(path, "t755_tmux_socket_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		src := string(body)
		rel, _ := filepath.Rel(root, path)
		for _, tok := range forbidden {
			if strings.Contains(src, tok) {
				hits = append(hits, rel+": "+tok)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) > 0 {
		t.Fatalf("production code regained a live-fleet tmux default path (🎯T755):\n  %s", strings.Join(hits, "\n  "))
	}
}

func repoRootForT755(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above cwd")
		}
		dir = parent
	}
}
