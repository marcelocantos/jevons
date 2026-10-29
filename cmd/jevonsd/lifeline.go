// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"io"
	"log/slog"
	"os"
	"syscall"
)

// lifelineEnv set to "stdin" makes the daemon stop when its stdin reaches
// end of file (🎯T1294). The journey suite sets it on every isolate and
// holds the write end of the pipe, so an isolate never outlives the suite.
const lifelineEnv = "JEVONS_LIFELINE"

// watchLifeline reads r until it ends, then requests a normal stop.
func watchLifeline(r io.Reader, stop chan<- os.Signal) {
	_, _ = io.Copy(io.Discard, r)
	slog.Warn("lifeline closed: the process that started this daemon is gone; stopping")
	stop <- syscall.SIGTERM
}
