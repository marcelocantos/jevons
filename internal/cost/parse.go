// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"encoding/json"
	"math"
	"time"
)

// jsonlLine is the subset of a Claude Code session-JSONL line that
// matters for billing. Everything else on the line is ignored.
type jsonlLine struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	SessionID string    `json:"sessionId"`
	RequestID string    `json:"requestId"`
	CostUSD   *float64  `json:"costUSD"` // older Claude Code versions precompute this
	Message   struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *Usage `json:"usage"`
	} `json:"message"`
}

// ParseLine extracts a billable Event from one JSONL line, or nil when
// the line carries no usage (user turns, tool results, meta lines,
// zero-usage synthetic messages). fallbackSession is used when the line
// omits sessionId (derived from the filename by the caller). now stamps
// lines with no timestamp.
//
// Transcript JSONL is untrusted-shaped (workers can write arbitrary
// lines). Negative or non-finite costUSD/token fields are rejected so a
// fabricated line cannot deflate SUM aggregates that drive the kill-
// switch and spawn-halt (T36.1 / Fable F1).
func ParseLine(line []byte, fallbackSession string, now time.Time) *Event {
	var l jsonlLine
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
		Timestamp: l.Timestamp,
		SessionID: l.SessionID,
		Model:     l.Message.Model,
		Usage:     u,
		RequestID: l.RequestID,
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = now
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

func usageNonNegative(u Usage) bool {
	return u.Input >= 0 && u.Output >= 0 && u.CacheCreate >= 0 && u.CacheRead >= 0
}

func costNonNegative(c float64) bool {
	return !math.IsNaN(c) && !math.IsInf(c, 0) && c >= 0
}
