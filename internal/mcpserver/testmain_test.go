// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/jevons/internal/spool"
	"github.com/marcelocantos/jevons/internal/tmpdirsweep"
)

// TestMain isolates the suite from the owner's live claudia state.
//
// It points the sidecar spool at an empty directory (🎯T910). spool.Dir
// falls back to ~/.jevons/spool, so any sweep that classified a Cursor seat
// scanned the development daemon's live spool — over a gigabyte, rescanned
// whenever the daemon appended. Tests that need spool records set their own
// directory with t.Setenv.
//
// It points Claudia's broker and sidecar sockets at paths nothing serves
// (🎯T975). A test that called jevons_agent_start with a registry not in direct
// mode granted a real seat on the owner's development broker, which resumed it
// on every restart and spent plan quota (2026-10-01).
//
// And it points claudia's tmux socket at a private path (🎯T740 / 🎯T755).
// Jevons no longer runs fleet pane census (🎯T459), but claudia's
// Registry.Remove still calls legacy tmuxagent.KillWindowsForSession on that
// socket. Left on the default it would reach the owner's real socket; left
// private and un-torn-down it orphaned one server per run (~200 on Colossus
// by 2026-10-14). Unguarded handleAgentList must never exec tmux kill-pane on
// a live fleet socket — TestMain's env is the suite-wide seam. The suite kills
// its own server before removing the directory.
func TestMain(m *testing.M) {
	sweepStaleTestDirs()

	dir, err := os.MkdirTemp("", "mcpserver-test-")
	if err != nil {
		panic(err)
	}
	sock := filepath.Join(dir, "tmux.sock")
	os.Setenv("CLAUDIA_TMUX_SOCKET", sock)
	os.Setenv(spool.DirEnv, filepath.Join(dir, "spool"))
	os.Setenv("CLAUDIA_BROKER_SOCKET", filepath.Join(dir, "no-broker.sock"))
	os.Setenv("CLAUDIA_OMP_SOCKET", filepath.Join(dir, "no-sidecar.sock"))
	code := m.Run()
	killPrivateTmuxServer(sock)
	os.RemoveAll(dir)
	os.Exit(code)
}

// mcpserverTempPrefixes are private suite roots under $TMPDIR. The legacy
// mcpserver-tmux-* name predates 🎯T579's rename to mcpserver-test-*.
var mcpserverTempPrefixes = []string{"mcpserver-test-", "mcpserver-tmux-"}

// sweepStaleTestDirs removes mcpserver-* directories left behind by a prior
// run whose process is no longer alive (🎯T1087 / 🎯T1168).
func sweepStaleTestDirs() {
	tmpdirsweep.SweepPrefixes(os.TempDir(), mcpserverTempPrefixes, func(dir string) {
		killPrivateTmuxServer(filepath.Join(dir, "tmux.sock"))
	})
}

// killPrivateTmuxServer stops the tmux server claudia may have started on
// this suite's own socket. Only that socket is addressed; no server is
// running is the common case and not an error.
func killPrivateTmuxServer(sock string) {
	if _, err := os.Stat(sock); err != nil {
		return
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		return
	}
	_ = exec.Command("tmux", "-S", sock, "kill-server").Run()
}
