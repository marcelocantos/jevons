// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"math"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"
)

// WithBands returns a copy of snap with every window's Band filled in at now.
//
// 🎯T610: the cockpit paints the daemon's verdict instead of computing one.
//
// The band on this payload IS the colour. The cockpit must not blend
// pressure, ratio, or anything else into a second colour for the same
// window. On 2026-09-24 that happened: the bar lerped an "ahead" band to
// green while the graph painted the band amber. Pressure is served so a
// reader can see the number behind the band. It is not a licence to repaint.
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
		if be.Available() {
			if strings.EqualFold(be.Provider, "claude") {
				be.Spend = spendIfActive(be.rawSpend, be.Windows)
			} else {
				be.Spend = nil
			}
		} else {
			be.Spend = nil
		}
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
				windows[j].Pressure = nil
				continue
			}
			windows[j].Band = string(BandOfWindow(windows[j], now, th))
			if be.Override != nil {
				windows[j].Band = string(be.Override.Band) // 🎯T948
			}
			windows[j].Pressure = pressureOfWindow(windows[j], now, th)
			windows[j].History = historyWithBands(windows[j], th)
		}
		be.Windows = windows
	}
	return out
}

// pressureOfWindow is the number BandOfWindow's overspend verdict came from,
// from the same inputs. Nil rather than a sentinel when there is nothing
// finite to serve: JSON cannot carry +Inf, and the band already says
// "exhausted".
func pressureOfWindow(w Window, now time.Time, th Thresholds) *float64 {
	used := usedPercent(w)
	rtp, hasTime := remainingTimePercent(w, now)
	if used == nil || !hasTime {
		return nil
	}
	p := Pressure(*used, 100-rtp, th)
	if math.IsInf(p, 0) || math.IsNaN(p) {
		return nil
	}
	return &p
}

// historyWithBands stamps each stored sample with the band the window had at
// that sample's moment (🎯T667): the sample's remaining as both remaining and
// 100-used, classified at the sample's own time against the same period. A
// copy — the history slice belongs to the shared snapshot.
//
// Same rule as the window's own Band, reached through the same function, so
// the sparkline's colour at its right edge is the bar's colour now.
func historyWithBands(w Window, th Thresholds) []HistoryPoint {
	if len(w.History) == 0 {
		return w.History
	}
	out := make([]HistoryPoint, len(w.History))
	for i, p := range w.History {
		rem := p.Remaining
		used := 100 - rem
		at := w
		at.RemainingPercent = &rem
		at.UsedPercent = &used
		at.History = nil
		p.Band = string(BandOfWindow(at, p.At, th))
		out[i] = p
	}
	return out
}

// spendIfActive gates Claude's provider-published extra-usage reading. At a
// full plan window a missing or changed block is unknown, not zero spend:
// show an explicit unavailable reason without inventing a dollar figure.
func spendIfActive(raw *claudia.PlanSpend, windows []Window) *BackendSpend {
	if !anyPlanWindowFull(windows) {
		return nil
	}
	if raw == nil {
		return &BackendSpend{UnavailableReason: "spend block missing from Claude usage reading"}
	}
	if !raw.Enabled && !raw.LimitReached {
		return nil
	}
	used := audMajorUnits(raw.Used)
	limit := audMajorUnits(raw.Limit)
	if used == nil || limit == nil {
		return &BackendSpend{UnavailableReason: "Claude spend money missing or currency not AUD", LimitReached: raw.LimitReached}
	}
	return &BackendSpend{UsedAUD: *used, LimitAUD: *limit, LimitReached: raw.LimitReached}
}

// anyPlanWindowFull reports whether the plan's own window (session or
// weekly, never a per-model window) is at 100% used right now.
func anyPlanWindowFull(windows []Window) bool {
	for _, w := range windows {
		if strings.EqualFold(w.Name, WindowModelWeekly) || strings.TrimSpace(w.Model) != "" {
			continue
		}
		if !strings.EqualFold(w.Name, WindowSession) && !strings.EqualFold(w.Name, WindowWeekly) {
			continue
		}
		if w.UsedPercent != nil && *w.UsedPercent >= 100 {
			return true
		}
	}
	return false
}

// audMajorUnits converts a provider money block to AUD major units
// (dollars), when the provider's own currency is already AUD. Nil when the
// block is absent or in a currency this package has no documented rate
// for — never a guessed conversion (🎯T967 currency rule).
func audMajorUnits(m *claudia.PlanMoney) *float64 {
	if m == nil || !strings.EqualFold(strings.TrimSpace(m.Currency), "AUD") {
		return nil
	}
	v := float64(m.AmountMinor) / math.Pow(10, float64(m.Exponent))
	return &v
}
