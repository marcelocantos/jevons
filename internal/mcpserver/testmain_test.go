// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/spool"
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
// And it points claudia's tmux socket at a private path. Jevons no longer
// touches tmux, but claudia v0.53's Registry.Remove still calls its legacy
// tmuxagent.KillWindowsForSession, which starts a "claudia-anchor" tmux server
// on that socket. Left on the default it would reach the owner's real socket;
// left private and un-torn-down it orphaned one server per run (~200 on
// Colossus by 2026-10-14). The suite kills its own server before removing the
// directory.
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

// sweepStaleTestDirs removes mcpserver-test-* directories left behind by a
// prior run of this suite whose process is no longer alive (🎯T1087): a
// binary killed by timeout or panic skips the os.RemoveAll in this
// TestMain's normal return path, and the dir never gets another chance to
// be cleaned since each run mints a fresh random name. A dir is only
// removed when its tmux.sock has no owning server, so a concurrently
// running suite's directory is left alone.
func sweepStaleTestDirs() {
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "mcpserver-test-") {
			continue
		}
		dir := filepath.Join(os.TempDir(), e.Name())
		sock := filepath.Join(dir, "tmux.sock")
		if _, err := os.Stat(sock); err == nil {
			killPrivateTmuxServer(sock)
		}
		os.RemoveAll(dir)
	}
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
