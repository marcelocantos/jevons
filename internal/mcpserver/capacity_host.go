// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"

	"github.com/marcelocantos/jevons/internal/capacity"
	"github.com/marcelocantos/jevons/internal/hostload"
)

// applyHostLoad copies one host reading into the capacity snapshot (🎯T463).
//
// This is the only place the two packages meet: hostload does the I/O, the
// capacity policy does the arithmetic, and neither knows about the other. A
// sample that failed to read carries zeroes, which the policy treats as
// unknown — the reading is never fabricated into a healthy number, because
// "nothing read the host" rendering as "the host is fine" is exactly how a
// 16-core machine was driven to a load average of 247 under a governor
// reporting normal.
func applyHostLoad(snap *capacity.Snapshot, s hostload.Sample) {
	snap.HostLoad1 = s.Load1
	snap.HostCores = s.Cores
	snap.HostSwapUsedBytes = s.SwapUsedBytes
	snap.HostSwapTotalBytes = s.SwapTotalBytes
	snap.HostSource = s.Source
	if s.Err != "" {
		snap.HostSource = s.Source + " (" + s.Err + ")"
	}
}

// hostLoadText renders the host dimension for jevons_capacity_status, or the
// empty string when nothing read it.
func hostLoadText(snap capacity.Snapshot) string {
	if snap.HostLoad1 <= 0 || snap.HostCores <= 0 {
		if snap.HostSource == "" {
			return ""
		}
		return fmt.Sprintf("  host: unread (%s) (🎯T463)\n", snap.HostSource)
	}
	s := fmt.Sprintf("  host: load1=%.1f on %d cores (%.1f× per core)",
		snap.HostLoad1, snap.HostCores, snap.HostLoad1/float64(snap.HostCores))
	if snap.HostSwapTotalBytes > 0 {
		s += fmt.Sprintf(", swap %.1fG of %.1fG (%.0f%%)",
			float64(snap.HostSwapUsedBytes)/(1<<30), float64(snap.HostSwapTotalBytes)/(1<<30),
			float64(snap.HostSwapUsedBytes)/float64(snap.HostSwapTotalBytes)*100)
	}
	if snap.HostSource != "" {
		s += " [" + snap.HostSource + "]"
	}
	return s + " (🎯T463)\n"
}
