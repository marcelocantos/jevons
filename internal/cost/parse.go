// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"bytes"
	"encoding/json"
	"math"
	"time"
)

// costUsdTicksPerUSD is how Grok Build encodes provider cost in
// updates.jsonl. When present this is preferred over the fallback rate
// table because it is the harness's own bill figure.
//
// The divisor is 1e10, not the 1e9 ("nano-dollars") this constant was
// first written as (🎯T394). It is not a fitted parameter: at 1e10 the
// ticks reproduce xAI's published grok-4.5 rate card as an exact
// arithmetic identity. Card (https://docs.x.ai/docs/models, read
// 2026-08-15), USD per million tokens:
//
//	prompt < 200k tokens:  input 2.00  cached input 0.30  output  6.00
//	prompt >= 200k tokens: input 4.00  cached input 0.60  output 12.00
//
// Method: over every grok-4.5-build turn_completed frame on this host
// with a single model call (n=1104 — one call so the frame's own token
// counts are the whole bill), compute
//
//	(input-cachedRead)*input_rate + cachedRead*cached_rate + output*output_rate
//
// and compare to costUsdTicks/1e10. 1044 frames agree to within a
// relative 1e-9; a further 51 agree exactly on the same card with cached
// input at 0.50/1.00, xAI's rate before it dropped to 0.30/0.60 on
// 2026-07-19. The remaining 9 differ only by a discrete non-token
// surcharge — always an exact multiple of $0.005 — never by a token-rate
// discrepancy. At 1e9 every one of those figures is 10x the published
// card, which is why the T36 clamp read phantom burn and the owner
// switched the meter off (budget.json, 2026-08-03).
//
// Pinned by TestT394GrokTicksDecodeMatchesPublishedRateCard over verbatim
// frames, with a control that fails at the old divisor.
const costUsdTicksPerUSD = 1e10

// claudeLine is the subset of a Claude Code session-JSONL line that
// matters for billing. Everything else on the line is ignored.
type claudeLine struct {
	Type      string          `json:"type"`
	Timestamp json.RawMessage `json:"timestamp"`
	SessionID string          `json:"sessionId"`
	RequestID string          `json:"requestId"`
	CostUSD   *float64        `json:"costUSD"` // older Claude Code versions precompute this
	Message   struct {
		ID         string `json:"id"`
		Model      string `json:"model"`
		StopReason string `json:"stop_reason"`
		Usage      *Usage `json:"usage"`
	} `json:"message"`
}

// grokUpdateLine is a Grok Build updates.jsonl envelope. Billable turns
// arrive as method _x.ai/session/update with sessionUpdate turn_completed
// and a usage block (camelCase token fields + optional costUsdTicks).
type grokUpdateLine struct {
	Timestamp json.RawMessage `json:"timestamp"`
	Method    string          `json:"method"`
	Params    struct {
		SessionID string `json:"sessionId"`
		Update    struct {
			SessionUpdate string `json:"sessionUpdate"`
			PromptID      string `json:"prompt_id"`
			StopReason    string `json:"stop_reason"`
			Usage         *struct {
				InputTokens         int64                      `json:"inputTokens"`
				OutputTokens        int64                      `json:"outputTokens"`
				CachedReadTokens    int64                      `json:"cachedReadTokens"`
				CacheCreationTokens int64                      `json:"cacheCreationTokens"`
				ModelCalls          int64                      `json:"modelCalls"`
				CostUsdTicks        *float64                   `json:"costUsdTicks"`
				ModelUsage          map[string]json.RawMessage `json:"modelUsage"`
			} `json:"usage"`
		} `json:"update"`
	} `json:"params"`
}

// ParseLine extracts a billable Event from one JSONL line, or nil when
// the line carries no usage (user turns, tool results, meta lines,
// zero-usage synthetic messages). fallbackSession is used when the line
// omits sessionId (derived from the path by the caller). now stamps
// lines with no timestamp.
//
// Supports two on-disk shapes:
//   - Claude Code assistant lines (message.usage + optional costUSD)
//   - Grok Build updates.jsonl turn_completed (params.update.usage +
//     optional costUsdTicks)
//
// Transcript JSONL is untrusted-shaped (workers can write arbitrary
// lines). Negative or non-finite costUSD/token fields are rejected so a
// fabricated line cannot deflate SUM aggregates that drive the kill-
// switch and spawn-halt (T36.1 / Fable F1).
func ParseLine(line []byte, fallbackSession string, now time.Time) *Event {
	if e := parseGrokUpdate(line, fallbackSession, now); e != nil {
		return e
	}
	return parseClaudeLine(line, fallbackSession, now)
}

func parseClaudeLine(line []byte, fallbackSession string, now time.Time) *Event {
	var l claudeLine
	if err := json.Unmarshal(line, &l); err != nil {
		return nil // partial/corrupt line — the tailer re-reads on next poll
	}
	if l.Message.Usage == nil {
		return nil
	}
	u := *l.Message.Usage
	if !usageNonNegative(u) {
		return nil
	}
	if u.IsZero() && l.CostUSD == nil {
		return nil
	}

	e := &Event{
		Timestamp: parseFlexibleTime(l.Timestamp, now),
		SessionID: l.SessionID,
		Model:     l.Message.Model,
		Usage:     u,
		RequestID: l.RequestID,
		// Claude Code bills one API call per assistant frame, so the frame
		// is the call and its input tokens are the context it carried.
		ModelCalls: 1,
		StopReason: l.Message.StopReason,
	}
	if e.SessionID == "" {
		e.SessionID = fallbackSession
	}
	if e.RequestID == "" {
		e.RequestID = l.Message.ID
	}
	if l.CostUSD != nil {
		if !costNonNegative(*l.CostUSD) {
			return nil
		}
		e.CostUSD = *l.CostUSD
	} else {
		e.CostUSD = EstimateCostUSD(e.Model, u)
	}
	return e
}

func parseGrokUpdate(line []byte, fallbackSession string, now time.Time) *Event {
	// Fast reject: Grok billable lines always carry turn_completed.
	// Avoids full struct decode on chat_history / Claude lines.
	if !bytes.Contains(line, []byte(`"turn_completed"`)) {
		return nil
	}
	var l grokUpdateLine
	if err := json.Unmarshal(line, &l); err != nil {
		return nil
	}
	if l.Params.Update.SessionUpdate != "turn_completed" || l.Params.Update.Usage == nil {
		return nil
	}
	gu := l.Params.Update.Usage
	u := Usage{
		Input:       gu.InputTokens,
		Output:      gu.OutputTokens,
		CacheCreate: gu.CacheCreationTokens,
		CacheRead:   gu.CachedReadTokens,
	}
	if !usageNonNegative(u) {
		return nil
	}
	if u.IsZero() && gu.CostUsdTicks == nil {
		return nil
	}

	e := &Event{
		Timestamp:  parseFlexibleTime(l.Timestamp, now),
		SessionID:  l.Params.SessionID,
		Model:      firstModel(gu.ModelUsage),
		Usage:      u,
		RequestID:  l.Params.Update.PromptID,
		ModelCalls: gu.ModelCalls,
		StopReason: l.Params.Update.StopReason,
	}
	// A turn always billed at least one call; a provider that omits the
	// count must not make the context per call read as zero (🎯T392.6).
	if e.ModelCalls <= 0 {
		e.ModelCalls = 1
	}
	if e.SessionID == "" {
		e.SessionID = fallbackSession
	}
	if e.Model == "" {
		e.Model = "grok"
	}
	if gu.CostUsdTicks != nil {
		if !costNonNegative(*gu.CostUsdTicks) {
			return nil
		}
		e.CostUSD = *gu.CostUsdTicks / costUsdTicksPerUSD
	} else {
		// Grok's inputTokens is inclusive of cachedReadTokens, so the
		// fresh-input count is the difference (🎯T394 — the published
		// card reproduces a frame's bill only on that reading). The Event
		// keeps the inclusive count, because Usage.Input is the context
		// the call carried (🎯T392.6); only the estimate splits it.
		fresh := u
		fresh.Input = max(0, u.Input-u.CacheRead)
		e.CostUSD = EstimateCostUSD(e.Model, fresh)
	}
	return e
}

func firstModel(m map[string]json.RawMessage) string {
	for name := range m {
		return name
	}
	return ""
}

// parseFlexibleTime accepts RFC3339 strings (Claude) or unix seconds /
// milliseconds numbers (Grok updates.jsonl). Unparseable → now.
func parseFlexibleTime(raw json.RawMessage, now time.Time) time.Time {
	if len(raw) == 0 || string(raw) == "null" {
		return now
	}
	// Quoted string → RFC3339 (or similar).
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil || s == "" {
			return now
		}
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t
		}
		return now
	}
	// Number: unix seconds, or ms if large.
	var n float64
	if err := json.Unmarshal(raw, &n); err != nil || n <= 0 {
		return now
	}
	// Threshold: values above 1e12 are almost certainly milliseconds
	// (year ~2001 in ms is ~1e12; year 2026 in seconds is ~1.78e9).
	if n > 1e12 {
		return time.UnixMilli(int64(n))
	}
	sec, frac := math.Modf(n)
	return time.Unix(int64(sec), int64(frac*1e9))
}

func usageNonNegative(u Usage) bool {
	return u.Input >= 0 && u.Output >= 0 && u.CacheCreate >= 0 && u.CacheRead >= 0
}

func costNonNegative(c float64) bool {
	return !math.IsNaN(c) && !math.IsInf(c, 0) && c >= 0
}
