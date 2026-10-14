// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import "github.com/marcelocantos/claudia"

// Thresholds is the plan-band vertex document GET /api/plan-usage/thresholds
// serves. It is a read-only view of Claudia's Spend Pressure Map (🎯T641),
// not a second source: every band vertex is claudia.DefaultPlanThresholds,
// and the band itself is claudia.ClassifyWindow / ClassifyPlan (🎯T1072).
// Nothing in Jevons or the cockpit classifies a window from these numbers.
type Thresholds struct {
	// LowRemainingPercent is Claudia's session remaining-low vertex.
	LowRemainingPercent float64 `json:"low_remaining_percent"`

	// ShrinkPriorK is the strength of the prior pulling the current-rate
	// estimate toward nominal, scaled by the time left (🎯T596).
	ShrinkPriorK float64 `json:"shrink_prior_k"`
	// PanicAmberLn / PanicRedLn are the ln(current/required) vertices for
	// burning too fast; WasteUnderLn / WasteLockedLn for leaving allowance
	// unspent. Waste is further out: running dry costs more.
	PanicAmberLn  float64 `json:"panic_amber_ln"`
	PanicRedLn    float64 `json:"panic_red_ln"`
	WasteUnderLn  float64 `json:"waste_under_ln"`
	WasteLockedLn float64 `json:"waste_locked_ln"`

	// MintIndifferencePercent is not a band vertex. It is Jevons' mint
	// policy: the weekly remaining-% gap under which two green providers
	// are an equally obvious omit-provider mint choice, so the
	// config.yaml / JEVONS_PROVIDER preference breaks the tie (🎯T495).
	MintIndifferencePercent float64 `json:"mint_indifference_percent"`
}

// DefaultMintIndifferencePercent is the 🎯T495 mint tie-break gap.
const DefaultMintIndifferencePercent = 10

// DefaultThresholds is Claudia's plan-band vertices plus Jevons' mint
// tie-break gap.
func DefaultThresholds() Thresholds {
	ct := claudia.DefaultPlanThresholds()
	return Thresholds{
		LowRemainingPercent:     ct.LowRemainingPercent,
		ShrinkPriorK:            ct.ShrinkPriorK,
		PanicAmberLn:            ct.PanicAmberLn,
		PanicRedLn:              ct.PanicRedLn,
		WasteUnderLn:            ct.WasteUnderLn,
		WasteLockedLn:           ct.WasteLockedLn,
		MintIndifferencePercent: DefaultMintIndifferencePercent,
	}
}

// DefaultWeeklyWindowSeconds is used when a weekly window publishes
// resets_at but no length (7d).
const DefaultWeeklyWindowSeconds int64 = 7 * 24 * 3600

// DefaultSessionWindowSeconds is used when a session window publishes
// resets_at but no length (5h).
const DefaultSessionWindowSeconds int64 = 5 * 3600
