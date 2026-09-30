// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/jevons/internal/spool"
)

// TestMain points the fleet tmux socket at a path nothing listens on
// (🎯T740). Server.paneIO falls back to the real socket when no test seam is
// wired, so any test that reaches handleAgentList / SweepOrphanPanes with a
// throwaway registry treated every live fleet pane as an orphan and ran
// `tmux kill-pane` on it — killing the developer's own seats, the harness
// running the suite included (exit 137), and stalling the suite. A dead
// socket makes list fail, so the census reaps nothing.
//
// It also points the sidecar spool at an empty directory (🎯T910). spool.Dir
// falls back to ~/.jevons/spool, so any sweep that classified a Cursor seat
// scanned the development daemon's live spool — over a gigabyte, rescanned
// whenever the daemon appended. Under -race that read outlasted the restart
// brief retry window and TestRestartBriefArrivesWhenTheProcessDoes failed.
// Tests that need spool records set their own directory with t.Setenv.
//
// And it points Claudia's broker and sidecar sockets at paths nothing serves
// (🎯T975). A test that called jevons_agent_start with a registry not in direct
// mode granted a real seat on the owner's development broker:
// TestAgentStartAllowsExistingWorkdir minted jv-t957-real on xAI Grok with
// AutoStart on every run, and the broker resumed it on each restart, spending
// a plan quota the owner had asked be left alone (2026-10-01).
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "mcpserver-tmux-")
	if err != nil {
		panic(err)
	}
	os.Setenv("CLAUDIA_TMUX_SOCKET", filepath.Join(dir, "dead.sock"))
	os.Setenv(spool.DirEnv, filepath.Join(dir, "spool"))
	os.Setenv("CLAUDIA_BROKER_SOCKET", filepath.Join(dir, "no-broker.sock"))
	os.Setenv("CLAUDIA_OMP_SOCKET", filepath.Join(dir, "no-sidecar.sock"))
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
