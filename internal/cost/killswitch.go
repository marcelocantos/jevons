// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// TmuxKillSwitch is the global stop for the claudia worker fleet: the
// tmux server on the fleet socket is launchd-parented, so it survives
// every closed terminal tab — exactly why the 2026-07-06 incident had
// to be killed by hand. This automates that kill.
//
// Scope is deliberate: jevons kills what it OWNS (everything hosted in
// the fleet's tmux server, registered or not), and only reports what it
// doesn't. It never pkills arbitrary claude processes — that could take
// out the owner's interactive sessions.
type TmuxKillSwitch struct {
	// Socket is the fleet tmux socket path. Empty resolves the claudia
	// default ($CLAUDIA_TMUX_SOCKET, else XDG state dir).
	Socket string
	// Grace is how long pane processes get to exit after the server
	// dies before surviving PIDs are SIGKILLed. Zero means 2s.
	Grace time.Duration
}

// ClaudiaSocketPath resolves the fleet tmux socket the same way claudia
// does: $CLAUDIA_TMUX_SOCKET, else $XDG_STATE_HOME/claudia/tmux.sock,
// else ~/.local/state/claudia/tmux.sock.
func ClaudiaSocketPath() string {
	if s := os.Getenv("CLAUDIA_TMUX_SOCKET"); s != "" {
		return s
	}
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "claudia", "tmux.sock")
}

// Kill terminates the fleet tmux server and everything in it, then
// SIGKILLs any pane process that survived, and removes the socket. It
// is idempotent: no server on the socket is success, not an error.
func (k *TmuxKillSwitch) Kill() error {
	sock := k.Socket
	if sock == "" {
		sock = ClaudiaSocketPath()
	}
	if sock == "" {
		return fmt.Errorf("killswitch: cannot resolve fleet tmux socket")
	}
	if _, err := os.Stat(sock); os.IsNotExist(err) {
		return nil // no fleet — nothing to kill
	}

	// Snapshot pane PIDs first: after kill-server there is nothing left
	// to ask, and pane processes only get SIGHUP, which a wedged process
	// can ignore.
	pids := k.panePIDs(sock)

	// kill-server errors when no server is listening; that's success.
	_ = exec.Command("tmux", "-S", sock, "kill-server").Run()
	os.Remove(sock)

	// Give processes their SIGHUP grace, then make death certain.
	grace := k.Grace
	if grace == 0 {
		grace = 2 * time.Second
	}
	deadline := time.Now().Add(grace)
	for {
		alive := survivors(pids)
		if len(alive) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			for _, pid := range alive {
				// Pane top processes lead their pty's process group;
				// killing the group catches their descendants too.
				_ = syscall.Kill(-pid, syscall.SIGKILL)
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
			// One more beat, then report anything unkillable.
			time.Sleep(200 * time.Millisecond)
			if left := survivors(alive); len(left) > 0 {
				return fmt.Errorf("killswitch: %d process(es) survived SIGKILL: %v", len(left), left)
			}
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// panePIDs lists the top-level process of every pane on the socket.
func (k *TmuxKillSwitch) panePIDs(sock string) []int {
	out, err := exec.Command("tmux", "-S", sock, "list-panes", "-a", "-F", "#{pane_pid}").Output()
	if err != nil {
		return nil
	}
	var pids []int
	for _, f := range strings.Fields(string(out)) {
		if pid, err := strconv.Atoi(f); err == nil {
			pids = append(pids, pid)
		}
	}
	return pids
}

// survivors filters pids down to those still alive.
func survivors(pids []int) []int {
	var alive []int
	for _, pid := range pids {
		// Signal 0 probes existence without touching the process.
		if err := syscall.Kill(pid, 0); err == nil {
			alive = append(alive, pid)
		}
	}
	return alive
}
