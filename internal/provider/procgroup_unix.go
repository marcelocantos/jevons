// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package provider

import (
	"os/exec"
	"syscall"
)

// setProcessGroup puts the provider in its own process group so the
// supervisor can signal the provider *and* anything it spawned. Without
// this, a provider that forks helpers leaves orphans behind on disable.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// terminateGroup asks the provider's process group to exit politely.
func terminateGroup(pid int) {
	signalGroup(pid, syscall.SIGTERM)
}

// killGroup ends the provider's process group unconditionally.
func killGroup(pid int) {
	signalGroup(pid, syscall.SIGKILL)
}

// signalGroup targets the negative pid (the group led by pid), falling
// back to the bare process if the group is already gone — a provider
// that outlived its group leader must still be reaped.
func signalGroup(pid int, sig syscall.Signal) {
	if pid <= 0 {
		return
	}
	if err := syscall.Kill(-pid, sig); err != nil {
		_ = syscall.Kill(pid, sig)
	}
}
