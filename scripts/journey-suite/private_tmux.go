// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"os/exec"
	"path/filepath"
)

// killPrivateClaudiaTmux stops the tmux server claudia may have started under
// a journey's own state root. Seats no longer use tmux, but claudia v0.53's
// Registry.Remove still calls its legacy tmuxagent.KillWindowsForSession,
// which starts a "claudia-anchor" server on $XDG_STATE_HOME/claudia/tmux.sock;
// removing the root without stopping it orphaned one server per run. Only the
// root's own socket is addressed.
func killPrivateClaudiaTmux(stateRoot string) {
	sock := filepath.Join(stateRoot, "claudia", "tmux.sock")
	if _, err := os.Stat(sock); err != nil {
		return
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		return
	}
	_ = exec.Command("tmux", "-S", sock, "kill-server").Run()
}
