// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"

	"github.com/marcelocantos/jevons/internal/testhygiene"
)

// sweepStaleJourneyIsolates reaps orphaned claudia-anchor servers and stale
// jevons-journey-* dirs left by a prior run that died without teardown
// (🎯T846). Called at suite test startup, not from the live journey binary.
func sweepStaleJourneyIsolates() {
	_, _ = testhygiene.SweepOrphanedJourneyAnchors()
	dir := os.TempDir()
	if td := os.Getenv("TMPDIR"); td != "" {
		dir = td
	}
	_, _ = testhygiene.SweepStaleJourneyDirs(dir)
}
