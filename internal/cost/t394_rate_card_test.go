// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"testing"
	"time"
)

// preT394Divisor is what costUsdTicksPerUSD was before 🎯T394: ticks read
// as nano-dollars, an assumption nothing ever checked. It survives here
// only as the control — every assertion below is re-run against it, and
// the test fails if the old divisor would also have passed.
const preT394Divisor = 1e9

// xAI's published grok-4.5 rate card, USD per million tokens, from
// https://docs.x.ai/docs/models (read 2026-08-15). The card is tiered by
// prompt size, and the tier boundary is the prompt token count:
//
//	prompt < 200k tokens:  input 2.00  cached input 0.30  output  6.00
//	prompt >= 200k tokens: input 4.00  cached input 0.60  output 12.00
//
// Cached input was 0.50 / 1.00 until xAI dropped it. The published card
// carries no changelog, so the date is read off this host's own frames:
// the last turn priced at the old cached rate is 2026-07-18 14:04 UTC and
// the first at the new one is 2026-07-19 02:41 UTC. Both eras appear in
// the fixture — a decode that only reproduces today's card would silently
// misprice every historical frame in the store.
type grokRateCard struct {
	tierBoundary       int64
	inputLo, inputHi   float64
	cachedLo, cachedHi float64
	outputLo, outputHi float64
}

var (
	cardCurrent  = grokRateCard{200000, 2.00, 4.00, 0.30, 0.60, 6.00, 12.00}
	cardPreJul19 = grokRateCard{200000, 2.00, 4.00, 0.50, 1.00, 6.00, 12.00}
)

// costUSD prices one turn's usage from the card. Grok's inputTokens is
// inclusive of cachedReadTokens (🎯T394), so the fresh-input count is the
// difference; charging both in full is what a naive reading does and it
// does not reproduce a single frame's bill. Grok reports no cache-creation
// tokens at all — 0 on every one of the 5,260 frames on this host — so the
// card has no cache-write rate to apply.
func (c grokRateCard) costUSD(u Usage) float64 {
	input, cached, output := c.inputLo, c.cachedLo, c.outputLo
	if u.Input >= c.tierBoundary {
		input, cached, output = c.inputHi, c.cachedHi, c.outputHi
	}
	fresh := u.Input - u.CacheRead
	return (float64(fresh)*input + float64(u.CacheRead)*cached + float64(u.Output)*output) / tokensPerMTok
}

// TestT394GrokTicksDecodeMatchesPublishedRateCard pins the costUsdTicks
// decode against xAI's published card over verbatim turn_completed frames
// copied out of ~/.grok/sessions. The frames carry usage only (no prompt
// or completion text), and each was chosen to exercise one arm of the
// card: below and above the 200k tier boundary, with and without cache
// reads, on both cached-rate eras.
//
// The claim under test is an arithmetic identity, not a fit: at the
// shipped divisor each frame's ticks equal the card's price for its own
// token counts to within a relative 1e-9. Across every single-model-call
// frame on this host (n=1104) it holds for 1095; the other 9 differ only
// by a discrete surcharge that is an exact multiple of $0.005, never by a
// token-rate discrepancy.
func TestT394GrokTicksDecodeMatchesPublishedRateCard(t *testing.T) {
	// Keyed by prompt_id, which ParseLine surfaces as Event.RequestID.
	cards := map[string]struct {
		card grokRateCard
		what string
	}{
		"117aa897-5f58-4924-b498-65a5a830e4df": {cardCurrent, "low tier, no cache read"},
		"9ece8ece-2724-4694-8373-e158893f24d4": {cardCurrent, "low tier, cache-dominated"},
		"7e3a22e8-57f8-4fb9-bc17-cad73261fb12": {cardCurrent, "high tier, cache-dominated"},
		"0330387d-594f-4d21-82c6-ea82d8639260": {cardPreJul19, "low tier, pre-2026-07-19 cached rate"},
	}

	f, err := os.Open("testdata/t394_grok_rate_card.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	seen := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		e := ParseLine(line, "fixture", time.Now())
		if e == nil {
			t.Fatalf("fixture frame not parsed as billable: %s", line)
		}
		c, ok := cards[e.RequestID]
		if !ok {
			t.Fatalf("fixture frame %s has no card expectation", e.RequestID)
		}
		want := c.card.costUSD(e.Usage)
		if math.Abs(e.CostUSD-want) > 1e-9*math.Max(1, want) {
			t.Errorf("%s (%s): decoded $%.9f, card says $%.9f (usage %+v)",
				e.RequestID, c.what, e.CostUSD, want, e.Usage)
		}
		// Control: the same ticks at the pre-T394 divisor. Without this,
		// the assertion above would pass for whatever divisor the code
		// happens to hold, since the card is only ever compared to it.
		atOldDivisor := e.CostUSD * (costUsdTicksPerUSD / preT394Divisor)
		if math.Abs(atOldDivisor-want) <= 1e-9*math.Max(1, want) {
			t.Errorf("%s (%s): control failed — the old 1e9 divisor also matches the card ($%.9f), so this test discriminates nothing",
				e.RequestID, c.what, atOldDivisor)
		}
		seen++
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if seen != len(cards) {
		t.Fatalf("fixture carried %d frames, want %d", seen, len(cards))
	}
}

// TestT394GrokFallbackEstimateMatchesCard covers the other half of the
// decode: a frame that carries no costUsdTicks at all falls through to the
// rate table, and that table used to price Grok at Anthropic-Opus rates
// (15/75) on the grounds that no public per-token list existed. One does,
// and it is the one the ticks bill at. The estimate must also split
// inputTokens, which is inclusive of cache reads — otherwise the fallback
// over-reports a cache-dominated turn several-fold, which is precisely the
// shape of turn a long-context fleet produces.
func TestT394GrokFallbackEstimateMatchesCard(t *testing.T) {
	usage := Usage{Input: 341537, Output: 85, CacheRead: 341120}
	line, err := json.Marshal(map[string]any{
		"timestamp": 1784547858,
		"method":    "_x.ai/session/update",
		"params": map[string]any{
			"sessionId": "s-no-ticks",
			"update": map[string]any{
				"sessionUpdate": "turn_completed",
				"prompt_id":     "p-no-ticks",
				"stop_reason":   "end_turn",
				"usage": map[string]any{
					"inputTokens":      usage.Input,
					"outputTokens":     usage.Output,
					"cachedReadTokens": usage.CacheRead,
					"modelCalls":       1,
					"modelUsage":       map[string]any{"grok-4.5-build": map[string]any{}},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	e := ParseLine(line, "fb", time.Now())
	if e == nil {
		t.Fatal("ticks-free Grok turn_completed not parsed as billable")
	}
	if e.Usage != usage {
		t.Fatalf("usage = %+v, want %+v — the Event keeps the inclusive input count (🎯T392.6)", e.Usage, usage)
	}
	// The table carries the high tier, so price the fixture against it
	// directly rather than through the tier test.
	want := (float64(usage.Input-usage.CacheRead)*4.00 +
		float64(usage.CacheRead)*0.60 +
		float64(usage.Output)*12.00) / tokensPerMTok
	if math.Abs(e.CostUSD-want) > 1e-9*math.Max(1, want) {
		t.Fatalf("fallback estimate = $%.9f, card says $%.9f", e.CostUSD, want)
	}
	// Control: the pre-T394 table (Opus rates, cache read at 0.1×, input
	// charged in full alongside it) is nowhere near.
	old := (float64(usage.Input)*15.00 +
		float64(usage.CacheRead)*15.00*0.1 +
		float64(usage.Output)*75.00) / tokensPerMTok
	if math.Abs(old-want) <= 1e-9*math.Max(1, want) {
		t.Fatalf("control failed — the old rate table also matches the card ($%.9f)", old)
	}
}
