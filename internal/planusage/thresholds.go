// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

// Thresholds are the daemon-owned transition points (🎯T390.1.6). The
// ticker paints from this document plus the live snapshot. Mint and
// migrate use the same numbers. They are not imported from JS.
type Thresholds struct {
	AheadRatio         float64 `json:"ahead_ratio"`
	HotRatio           float64 `json:"hot_ratio"`
	UnderWastePercent  float64 `json:"under_waste_percent"`
	LockedWastePercent float64 `json:"locked_waste_percent"`
	// WarmupElapsedPercent is served for document compat. Colour and
	// WeeklyBandOf do not short-circuit on it (🎯T390.1.6.2) — damping
	// is the only early-window ease.
	WarmupElapsedPercent     float64 `json:"warmup_elapsed_percent"`
	LowRemainingPercent      float64 `json:"low_remaining_percent"`
	CriticalRemainingPercent float64 `json:"critical_remaining_percent"`

	// MintIndifferencePercent is the weekly remaining-% gap under which
	// two green providers are an equally obvious omit-provider mint
	// choice — only then does the config.yaml / JEVONS_PROVIDER
	// preference break the tie (🎯T495).
	MintIndifferencePercent float64 `json:"mint_indifference_percent"`

	// DampLambdaPercent is the additive damping λ applied to both terms
	// of the burn ratio: burn = (used% + λ) / (elapsed% + λ)
	// (🎯T390.1.6.1). Early in a window the raw ratio is a tiny-sample
	// artefact — 9% used at 5.6% elapsed is burn 1.6 and painted a
	// barely-started week red, which then drove migrate-off from a
	// backend with 91% remaining. λ pulls small samples toward the
	// neutral 1.0 while leaving mid-window readings on their side of
	// the vertices: 9/5.6 damps to 1.32 (ahead), 80/50 damps to 1.55
	// (still hot). λ must stay below 10, or 80/50 crosses under the
	// 1.5 hot vertex. Waste arithmetic (under/locked) stays raw.
	DampLambdaPercent float64 `json:"damp_lambda_percent"`
}

// DefaultThresholds matches the vertices the cockpit already used
// (ahead 1.0, hot 1.5, waste 15, remaining-low 15 / 5, damp λ 5).
// warmup_elapsed_percent stays in the document at 5 but is unused
// by WeeklyBandOf / classifyPace.
func DefaultThresholds() Thresholds {
	return Thresholds{
		AheadRatio:               1.0,
		HotRatio:                 1.5,
		UnderWastePercent:        15,
		LockedWastePercent:       15,
		WarmupElapsedPercent:     5,
		LowRemainingPercent:      15,
		CriticalRemainingPercent: 5,
		MintIndifferencePercent:  10,
		DampLambdaPercent:        5,
	}
}

// DefaultWeeklyWindowSeconds is used when a weekly window publishes
// resets_at but no length (7d).
const DefaultWeeklyWindowSeconds int64 = 7 * 24 * 3600

// DefaultSessionWindowSeconds is used when a session window publishes
// resets_at but no length (5h).
const DefaultSessionWindowSeconds int64 = 5 * 3600
