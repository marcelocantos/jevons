// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package seatload

import "syscall"

// hostSignal delivers sig. A negative pid targets the process group, which
// is how a reap reaches a shell's whole job at once.
func hostSignal(pid int, sig syscall.Signal) error { return syscall.Kill(pid, sig) }
