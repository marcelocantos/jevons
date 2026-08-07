// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build !unix

package provider

import (
	"os"
	"os/exec"
)

// setProcessGroup is a no-op off unix: process groups are the unix
// mechanism for reaping a provider's descendants, and platforms without
// them fall back to signalling the provider itself.
func setProcessGroup(cmd *exec.Cmd) {}

// terminateGroup asks the provider to exit politely.
func terminateGroup(pid int) { signalProcess(pid, os.Interrupt) }

// killGroup ends the provider unconditionally.
func killGroup(pid int) { signalProcess(pid, os.Kill) }

func signalProcess(pid int, sig os.Signal) {
	if pid <= 0 {
		return
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	_ = p.Signal(sig)
}
