// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

// Thresholds are the daemon-owned transition points (🎯T390.1.6). The
// ticker paints from this document plus the live snapshot. Mint and
// migrate use the same numbers. They are not imported from JS.
type Thresholds struct {
	AheadRatio               float64 `json:"ahead_ratio"`
	HotRatio                 float64 `json:"hot_ratio"`
	UnderWastePercent        float64 `json:"under_waste_percent"`
	LockedWastePercent       float64 `json:"locked_waste_percent"`
	WarmupElapsedPercent     float64 `json:"warmup_elapsed_percent"`
	LowRemainingPercent      float64 `json:"low_remaining_percent"`
	CriticalRemainingPercent float64 `json:"critical_remaining_percent"`
}

// DefaultThresholds matches the vertices the cockpit already used
// (ahead 1.0, hot 1.5, waste 15, warmup 5, remaining-low 15 / 5).
func DefaultThresholds() Thresholds {
	return Thresholds{
		AheadRatio:               1.0,
		HotRatio:                 1.5,
		UnderWastePercent:        15,
		LockedWastePercent:       15,
		WarmupElapsedPercent:     5,
		LowRemainingPercent:      15,
		CriticalRemainingPercent: 5,
	}
}

// DefaultWeeklyWindowSeconds is used when a weekly window publishes
// resets_at but no length (7d).
const DefaultWeeklyWindowSeconds int64 = 7 * 24 * 3600

// DefaultSessionWindowSeconds is used when a session window publishes
// resets_at but no length (5h).
const DefaultSessionWindowSeconds int64 = 5 * 3600
