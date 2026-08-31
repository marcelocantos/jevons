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
	// WarmupElapsedPercent is how much of a window must have passed
	// before a burning-fast verdict is believable at all (🎯T595).
	//
	// It was inert from 🎯T390.1.6.2 until 🎯T595 — published, parsed, and
	// read by nothing — on the theory that damping alone eased the early
	// window. It does not. As elapsed approaches 0 the damped burn
	// collapses to 1 + used/λ, which contains no rate at all: with λ=5,
	// "hot" simply means used ≥ 2.5pp, whenever that happens. On
	// 2026-08-31 claude's week was painted red at 94% remaining, 2.13%
	// into the window, because 6% used damps to 1.544.
	//
	// EarlyAlarmUsedPercent is the escape hatch, so warmup cannot mute a
	// genuine emergency: spend that much of a window before warmup and the
	// verdict lands anyway.
	WarmupElapsedPercent     float64 `json:"warmup_elapsed_percent"`
	EarlyAlarmUsedPercent    float64 `json:"early_alarm_used_percent"`
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

	// AheadMarginPercent is the percentage-point margin a window must
	// overspend by before any burning-fast verdict (ahead or hot) is
	// reachable: used% - elapsed% must exceed it (🎯T591).
	//
	// Damping alone cannot do this. (used+λ)/(elapsed+λ) approaches 1
	// from above and never crosses it, so with AheadRatio at exactly 1.0
	// ANY overspend at all — including one made entirely of rounding —
	// paints the window amber, however large λ is. On 2026-08-31 claude's
	// week read used 1% against elapsed 0.38% and went amber seven
	// minutes into a seven-day window: providers publish used as whole
	// percentage points, so the numerator's quantum was larger than the
	// denominator's value.
	//
	// A margin is the right shape because the noise is absolute, not
	// proportional. 2pp keeps both vertices the damping comment cites:
	// 9% at 5.6% elapsed overspends by 3.4pp and stays ahead, 80/50 by
	// 30pp and stays hot.
	AheadMarginPercent float64 `json:"ahead_margin_percent"`

	// 🎯T596 pressure model. Colour answers how large a correction the
	// window demands, not where the current rate would land.
	//
	// ShrinkPriorK is the strength of the prior that pulls the
	// current-rate estimate toward nominal, scaled by the time left. A
	// fan-out sprint in a window's first minutes is not a policy, and it
	// is also trivially correctable; both facts argue for discounting it,
	// and both stop applying as the deadline nears.
	ShrinkPriorK float64 `json:"shrink_prior_k,omitempty"`
	// PanicAmberLn / PanicRedLn are ln(current/required) vertices for
	// burning too fast.
	PanicAmberLn float64 `json:"panic_amber_ln,omitempty"`
	PanicRedLn   float64 `json:"panic_red_ln,omitempty"`
	// WasteUnderLn / WasteLockedLn are the same for the opposite failure,
	// and are deliberately further out: running dry costs more than
	// leaving allowance unspent, so waste gets more room before it speaks.
	WasteUnderLn  float64 `json:"waste_under_ln,omitempty"`
	WasteLockedLn float64 `json:"waste_locked_ln,omitempty"`
}

// 🎯T596 defaults, calibrated against real windows: a five-minute fan-out
// sprint reads 0.10; identical conduct sustained reads 0.16 on day 2, 0.40
// on day 4, 1.02 on day 6 — same behaviour, rising alarm as the runway
// shortens. 80/50 reads 1.27; a window 2% from dry with 5% of its time
// left reads 0.95, which the old statistic scored 1.03 and painted green.
const (
	DefaultShrinkPriorK  = 40.0
	DefaultPanicAmberLn  = 0.25
	DefaultPanicRedLn    = 0.85
	DefaultWasteUnderLn  = -0.60
	DefaultWasteLockedLn = -2.00
)

// DefaultThresholds matches the vertices the cockpit already used
// (ahead 1.0, hot 1.5, waste 15, remaining-low 15 / 5, damp λ 5).
// warmup_elapsed_percent is 5 and load-bearing again since 🎯T595.
func DefaultThresholds() Thresholds {
	return Thresholds{
		AheadRatio:               1.0,
		HotRatio:                 1.5,
		UnderWastePercent:        15,
		LockedWastePercent:       15,
		WarmupElapsedPercent:     5,
		EarlyAlarmUsedPercent:    25,
		LowRemainingPercent:      15,
		CriticalRemainingPercent: 5,
		MintIndifferencePercent:  10,
		DampLambdaPercent:        5,
		AheadMarginPercent:       2,
		ShrinkPriorK:             DefaultShrinkPriorK,
		PanicAmberLn:             DefaultPanicAmberLn,
		PanicRedLn:               DefaultPanicRedLn,
		WasteUnderLn:             DefaultWasteUnderLn,
		WasteLockedLn:            DefaultWasteLockedLn,
	}
}

// DefaultWeeklyWindowSeconds is used when a weekly window publishes
// resets_at but no length (7d).
const DefaultWeeklyWindowSeconds int64 = 7 * 24 * 3600

// DefaultSessionWindowSeconds is used when a session window publishes
// resets_at but no length (5h).
const DefaultSessionWindowSeconds int64 = 5 * 3600
