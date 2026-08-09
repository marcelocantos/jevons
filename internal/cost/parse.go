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
// updates.jsonl: costUsdTicks / 1e9 = USD. Observed on live sessions
// (nano-dollar ticks); when present this is preferred over the fallback
// rate table because it is the harness's own bill figure.
const costUsdTicksPerUSD = 1e9

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
		e.CostUSD = EstimateCostUSD(e.Model, u)
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
