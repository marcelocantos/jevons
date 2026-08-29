// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package capacity

// MaxStuckBusyScale caps how far host load may stretch the stuck-busy
// watchdog. A host at 70/16 cores (4.4 per core) makes every seat slow,
// not stuck; a runaway box should still be judged eventually (🎯T567).
const MaxStuckBusyScale = 8.0

// StuckBusyScale returns the multiplier the overseer's stuck-busy timeout
// should be stretched by under host load (🎯T567, riding the 🎯T566.1
// per-core reading). A run queue at or under one per core is a normal
// host (scale 1); above that the threshold grows linearly with load per
// core, capped at MaxStuckBusyScale. Unknown load (zero) is scale 1 —
// the watchdog never stretches on missing data.
func StuckBusyScale(load1 float64, cores int) float64 {
	if load1 <= 0 || cores <= 0 {
		return 1
	}
	perCore := load1 / float64(cores)
	if perCore <= 1 {
		return 1
	}
	if perCore > MaxStuckBusyScale {
		return MaxStuckBusyScale
	}
	return perCore
}
