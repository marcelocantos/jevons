// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"sync"
)

// The suite's lifeline (🎯T1294). Every isolate daemon gets the read end as
// stdin with JEVONS_LIFELINE=stdin; the write end stays open in this process
// and is never written. However the suite ends — a clean exit, SIGKILL, a
// crash, its shell killed — the kernel closes the write end and every
// isolate reads end of file and stops. exitCleanup covers only the exits
// that unwind; on 2026-10-29 isolates from killed runs were found 15 hours
// later, still running their seats and auditing on a paid model.
var (
	lifelineOnce sync.Once
	lifelineR    *os.File
	lifelineW    *os.File // held, never written, until the process ends
	lifelineErr  error
)

// lifelineReader returns the read end of the suite's lifeline pipe.
func lifelineReader() (*os.File, error) {
	lifelineOnce.Do(func() {
		lifelineR, lifelineW, lifelineErr = os.Pipe()
	})
	return lifelineR, lifelineErr
}
