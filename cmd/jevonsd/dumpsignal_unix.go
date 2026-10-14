// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package main

import (
	"os"
	"syscall"
)

// dumpSignal is SIGUSR1: `kill -USR1 <pid>` writes a diagnostic dump.
var dumpSignal os.Signal = syscall.SIGUSR1
