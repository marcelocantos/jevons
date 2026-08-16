// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import "strings"

// ModelRate is USD per million tokens for one model family. Cache write
// derives from the input rate using Anthropic's standard 1.25× multiplier.
// Cache read does not: it is 0.1× of input on Anthropic families but
// 0.15× on Grok, so it is carried per family rather than assumed (🎯T394).
type ModelRate struct {
	InputPerMTok  float64
	OutputPerMTok float64
	// CacheReadMultiplier scales InputPerMTok for cache-read tokens.
	// Zero means the Anthropic default (cacheReadMultiplier).
	CacheReadMultiplier float64
}

// CacheReadPerMTok is the cache-read rate for this family.
func (r ModelRate) CacheReadPerMTok() float64 {
	m := r.CacheReadMultiplier
	if m == 0 {
		m = cacheReadMultiplier
	}
	return r.InputPerMTok * m
}

// rateTable maps a lowercase model-name substring to its rates. Matched
// in order; first hit wins. Provider-supplied cost (Claude costUSD, Grok
// costUsdTicks) is always preferred when present; this table is only the
// fallback, and drift here biases estimates, not billing.
var rateTable = []struct {
	substr string
	rate   ModelRate
}{
	{"opus", ModelRate{InputPerMTok: 15, OutputPerMTok: 75}},
	{"sonnet", ModelRate{InputPerMTok: 3, OutputPerMTok: 15}},
	{"haiku", ModelRate{InputPerMTok: 1, OutputPerMTok: 5}},
	// Grok Build: xAI's published grok-4.5 card, the >=200k-prompt tier
	// (🎯T394 — the card is cited in full on costUsdTicksPerUSD). This
	// entry used to be a 15/75 guess justified as "no public per-token
	// list"; there is one, and it is what costUsdTicks bills at. The
	// high tier is the conservative choice of the card's two: a fleet
	// worker's prompt is over 200k far more often than not, and this
	// table is only reached when a frame carries no ticks of its own.
	{"grok", ModelRate{InputPerMTok: 4, OutputPerMTok: 12, CacheReadMultiplier: 0.15}},
}

// defaultRate prices unknown models (new families) at the most expensive
// known family. For a clamp-down system, overestimating unknown spend is
// the safe direction — it trips budgets earlier, never later.
var defaultRate = ModelRate{InputPerMTok: 15, OutputPerMTok: 75}

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
		float64(u.CacheRead)*r.CacheReadPerMTok()) / tokensPerMTok
}
