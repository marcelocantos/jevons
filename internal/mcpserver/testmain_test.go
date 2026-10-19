// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
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
// directory, and arms a pipe-watcher so a -timeout / SIGKILL still reaps it
// (🎯T846) — t.Cleanup and the code after m.Run do not run on those paths.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "mcpserver-test-")
	if err != nil {
		panic(err)
	}
	sock := filepath.Join(dir, "tmux.sock")
	os.Setenv("CLAUDIA_TMUX_SOCKET", sock)
	os.Setenv("JEVONS_SPOOL_DIR", filepath.Join(dir, "spool"))
	os.Setenv("CLAUDIA_BROKER_SOCKET", filepath.Join(dir, "no-broker.sock"))
	os.Setenv("CLAUDIA_OMP_SOCKET", filepath.Join(dir, "no-sidecar.sock"))

	// Watcher holds the read end; we hold the write end. Any death of this
	// binary closes the write end and the watcher kills our private tmux.
	reapDone := armPrivateTmuxReap(sock)

	code := m.Run()
	killPrivateTmuxServer(sock)
	// Close our end so the watcher exits cleanly if it is still waiting.
	if reapDone != nil {
		reapDone()
	}
	os.RemoveAll(dir)
	os.Exit(code)
}

// armPrivateTmuxReap starts a detached watcher that runs
// `tmux -S sock kill-server` when this process dies. Returns a stop func
// that closes the pipe early (normal path after m.Run already killed tmux).
func armPrivateTmuxReap(sock string) func() {
	tmux, err := exec.LookPath("tmux")
	if err != nil {
		return nil
	}
	r, w, err := os.Pipe()
	if err != nil {
		return nil
	}
	// Block on stdin EOF, then kill-server. exec so the shell is gone before
	// pkill-shaped accidents; argv of the watcher must NOT contain sock in a
	// way that matches unrelated processes — we invoke tmux directly on the
	// specific socket only.
	watcher := exec.Command("/bin/sh", "-c", `read -r _ || true; exec "$1" -S "$2" kill-server`,
		"mcpserver-tmux-reap", tmux, sock)
	watcher.Stdin = r
	watcher.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := watcher.Start(); err != nil {
		r.Close()
		w.Close()
		return nil
	}
	r.Close()
	done := make(chan struct{})
	go func() { _ = watcher.Wait(); close(done) }()
	return func() {
		_ = w.Close()
		<-done
	}
}

// killPrivateTmuxServer stops the tmux server claudia may have started on
// this suite's own socket. Only that socket is addressed; no server is
// running is the common case and not an error.
func killPrivateTmuxServer(sock string) {
	if _, err := os.Stat(sock); err != nil {
		// Socket file may already be gone while the server still holds it;
		// still try kill-server.
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		return
	}
	_ = exec.Command("tmux", "-S", sock, "kill-server").Run()
}
