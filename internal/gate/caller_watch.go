// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"os"
	"os/exec"
	"sync/atomic"
	"syscall"
	"time"
)

// CallerGoneNote is the status note for a run stopped because the process
// that invoked bin/gate exited (🎯T1132).
const CallerGoneNote = "caller_gone"

// callerWatchInterval is how often Run polls while the gated command runs.
// The acceptance oracle allows 2s for the tree to die after the wrapper goes.
const callerWatchInterval = 50 * time.Millisecond

// disownFromCallerProcessGroup starts a new session so a wrapper SIGKILL to
// the invoking shell's process group does not take gate with it — only
// CallerGone stops the run (🎯T1132).
func disownFromCallerProcessGroup() {
	_, _ = syscall.Setsid()
}

// CallerPID is the parent process at gate start — who the gate watches.
func CallerPID() int {
	return os.Getppid()
}

// CallerGone reports whether the invoking process is gone: reparented to pid 1
// or the pid recorded at start no longer exists (🎯T1132).
func CallerGone(callerPID int) bool {
	if callerPID <= 1 {
		return false
	}
	if os.Getppid() == 1 {
		return true
	}
	if err := syscall.Kill(callerPID, 0); err == syscall.ESRCH {
		return true
	}
	return false
}

// setGateCommandGroup puts the gated command in its own process group so the
// gate can terminate the whole tree when the caller disappears.
func setGateCommandGroup(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killCommandGroup ends the gated command's process group unconditionally.
func killCommandGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

// startGatedCommand runs cmd while watching callerPID; when the caller dies
// the command's process group is killed and callerStopped is set.
func startGatedCommand(cmd *exec.Cmd, callerPID int, callerStopped *atomic.Bool) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	stopWatch := make(chan struct{})
	go watchCaller(callerPID, cmd, callerStopped, stopWatch)
	err := cmd.Wait()
	close(stopWatch)
	return err
}

func watchCaller(callerPID int, cmd *exec.Cmd, stopped *atomic.Bool, stop <-chan struct{}) {
	ticker := time.NewTicker(callerWatchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if CallerGone(callerPID) {
				stopped.Store(true)
				killCommandGroup(cmd)
				return
			}
		}
	}
}
