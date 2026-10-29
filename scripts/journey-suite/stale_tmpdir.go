// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"time"

	"github.com/marcelocantos/jevons/internal/tmpdirsweep"
)

// orphanStopGrace lets signalled orphans exit before their trees are swept.
const orphanStopGrace = 3 * time.Second

// journeyTempPrefixes are isolate trees the suite creates under $TMPDIR.
// jevons-journey-bin-* lived beside state before 🎯T1106 folded builds in.
var journeyTempPrefixes = []string{
	"jevons-journey-",
	"jevons-journey-bin-",
	"jevons-broker-journey-",
	"jevons-broker-",
	"jevons-trust-journey-",
}

// sweepStaleIsolateDirs removes journey isolate directories left when a prior
// run was SIGKILL'd and could not run cleanups (🎯T1168). It first stops the
// orphaned daemons and brokers still running from those trees (🎯T1294);
// isolate daemons since then stop on their own when the suite's lifeline
// closes, but a claudia broker has no lifeline to read.
func sweepStaleIsolateDirs() {
	if pids := tmpdirsweep.StopOrphans(os.TempDir(), journeyTempPrefixes); len(pids) > 0 {
		fmt.Printf("stopped %d orphaned isolate process(es) from earlier runs: %v\n", len(pids), pids)
		time.Sleep(orphanStopGrace)
	}
	tmpdirsweep.SweepPrefixes(os.TempDir(), journeyTempPrefixes, killPrivateClaudiaTmux)
}
