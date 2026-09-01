// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import "time"

// WithBands returns a copy of snap with every window's Band filled in at now.
//
// 🎯T610: the cockpit paints the daemon's verdict instead of computing one.
// Before this, GET /api/plan-usage carried only used_percent,
// remaining_percent, resets_at and limit_window_seconds — raw numbers with no
// verdict — so the browser had no choice but to classify for itself, and
// ui/src/plan/pace.ts grew a second copy of the model. When 🎯T596 replaced
// the ratio bands with the pressure model in Go, the copy kept the old rule
// and the two disagreed on a real window.
//
// A COPY, because the snapshot is shared: the source hands the same value to
// every reader, and filling bands in place would be a write on a struct other
// goroutines are reading.
//
// At now, because the verdict is time-dependent. Pressure divides by the time
// left in the window, so a snapshot that has not changed still classifies
// differently an hour later. Storing the band on the snapshot when it is
// fetched would serve a verdict that ages; computing it here means the answer
// is always as of the request.
func WithBands(snap Snapshot, now time.Time, th Thresholds) Snapshot {
	if len(snap.Backends) == 0 {
		return snap
	}
	out := snap
	out.Backends = make([]Backend, len(snap.Backends))
	copy(out.Backends, snap.Backends)

	for i := range out.Backends {
		be := &out.Backends[i]
		if len(be.Windows) == 0 {
			continue
		}
		windows := make([]Window, len(be.Windows))
		copy(windows, be.Windows)
		for j := range windows {
			// An unavailable backend publishes no verdict rather than a
			// cheerful default: "we cannot see this provider" and "this
			// provider is fine" must not paint the same.
			if !be.Available() {
				windows[j].Band = ""
				continue
			}
			windows[j].Band = string(BandOfWindow(windows[j], now, th))
		}
		be.Windows = windows
	}
	return out
}
