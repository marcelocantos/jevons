// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package upgrade

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// HandoffTransportAlive verifies the upgrade snapshot against the current
// process/window before retaining a legacy CLI transport. A snapshot is a
// hint, not liveness: a dead PID or vanished tmux window must remint cold.
func HandoffTransportAlive(h Handle) bool {
	return handoffTransportAlive(h, func(pid int) bool { return syscall.Kill(pid, 0) == nil }, func(id string) bool {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		socket := os.Getenv("CLAUDIA_TMUX_SOCKET")
		if socket == "" {
			state := os.Getenv("XDG_STATE_HOME")
			if state == "" {
				state = filepath.Join(os.Getenv("HOME"), ".local", "state")
			}
			socket = filepath.Join(state, "claudia", "tmux.sock")
		}
		out, err := exec.CommandContext(ctx, "tmux", "-S", socket, "display-message", "-p", "-t", id, "#{window_id}").Output()
		return err == nil && strings.TrimSpace(string(out)) == id
	})
}

func handoffTransportAlive(h Handle, pidAlive func(int) bool, windowAlive func(string) bool) bool {
	if ShouldStopOnUpgrade(h, true) {
		return false
	}
	if h.ConnectURL != "" && h.PID > 0 {
		return pidAlive(h.PID)
	}
	if tmuxAdoptableWindow(h.TmuxWindowID) {
		return windowAlive(h.TmuxWindowID)
	}
	return false
}
