// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import "strings"

// ModelRate is USD per million tokens for one model family. Cache rates
// derive from the input rate using Anthropic's standard multipliers
// (write = 1.25×, read = 0.1×), which hold across current families.
type ModelRate struct {
	InputPerMTok  float64
	OutputPerMTok float64
}

// rateTable maps a lowercase model-name substring to its rates. Matched
// in order; first hit wins. The entries reflect Anthropic list pricing;
// costUSD from the transcript is always preferred when present, so this
// table is the fallback, and drift here biases estimates, not billing.
var rateTable = []struct {
	substr string
	rate   ModelRate
}{
	{"opus", ModelRate{15, 75}},
	{"sonnet", ModelRate{3, 15}},
	{"haiku", ModelRate{1, 5}},
}

// defaultRate prices unknown models (new families, e.g. fable) at the
// most expensive known family. For a clamp-down system, overestimating
// unknown spend is the safe direction — it trips budgets earlier, never
// later.
var defaultRate = ModelRate{15, 75}

const (
	cacheWriteMultiplier = 1.25
	cacheReadMultiplier  = 0.1
	tokensPerMTok        = 1e6
)

// RateFor returns the pricing for a model name.
func RateFor(model string) ModelRate {
	m := strings.ToLower(model)
	for _, e := range rateTable {
		if strings.Contains(m, e.substr) {
			return e.rate
		}
	}
	return defaultRate
}

// EstimateCostUSD prices a usage record from the fallback table. Use only
// when the transcript line carries no costUSD of its own.
func EstimateCostUSD(model string, u Usage) float64 {
	r := RateFor(model)
	return (float64(u.Input)*r.InputPerMTok +
		float64(u.Output)*r.OutputPerMTok +
		float64(u.CacheCreate)*r.InputPerMTok*cacheWriteMultiplier +
		float64(u.CacheRead)*r.InputPerMTok*cacheReadMultiplier) / tokensPerMTok
}
