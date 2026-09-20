// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain points the fleet tmux socket at a path nothing listens on
// (🎯T740). Server.paneIO falls back to the real socket when no test seam is
// wired, so any test that reaches handleAgentList / SweepOrphanPanes with a
// throwaway registry treated every live fleet pane as an orphan and ran
// `tmux kill-pane` on it — killing the developer's own seats, the harness
// running the suite included (exit 137), and stalling the suite. A dead
// socket makes list fail, so the census reaps nothing.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "mcpserver-tmux-")
	if err != nil {
		panic(err)
	}
	os.Setenv("CLAUDIA_TMUX_SOCKET", filepath.Join(dir, "dead.sock"))
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
