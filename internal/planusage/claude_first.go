// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"strings"
	"time"
)

// ClaudeHarness is the claudia backend id the owner rule names (🎯T561).
const ClaudeHarness = "claude"

// ClaudeFirstDecision answers "may an omit-provider mint land on Claude?"
// (🎯T583). The owner rule is Claude unless Claude is exhausted or blocked
// — it is not the usage-first "greenest dest" question T495 answers, so a
// merely busier Claude still wins over a fresher Grok.
type ClaudeFirstDecision struct {
	// OK is true when Claude is published and neither exhausted nor blocked.
	OK bool
	// Headroom is the weekly remaining % when published (session remaining
	// as the fallback figure). Nil means published-but-unquantified.
	Headroom *float64
	// Reason names why Claude was passed over: exhausted, blocked,
	// session_low, absent. Empty when OK.
	Reason string
}

// ClaudeFirst classifies the Claude backend in a plan-usage candidate set.
//
// Exhausted covers a 429/rate_limit reason, a 0% weekly or session window,
// and a session at or under the low-remaining threshold (that window empties
// mid-turn, so minting into it is the same failure as minting into zero).
// Blocked covers an unavailable backend — not signed in, unpublished. Absent
// means the feed carried no Claude row at all: unknown, and unknown is not
// a licence to claim headroom.
func ClaudeFirst(cands []DestCand, now time.Time, th Thresholds) ClaudeFirstDecision {
	for _, c := range cands {
		p := strings.ToLower(strings.TrimSpace(c.Provider))
		if p == "" {
			p = strings.ToLower(strings.TrimSpace(c.Backend.Provider))
		}
		if p != ClaudeHarness {
			continue
		}
		be := c.Backend
		// 🎯T677: a failed reading is "blocked" — we could not tell — not
		// "exhausted", which is a claim about the allowance itself.
		if !be.Available() {
			return ClaudeFirstDecision{Reason: "blocked"}
		}
		switch SessionStatusOf(be, th) {
		case SessionExhausted:
			return ClaudeFirstDecision{Reason: "exhausted"}
		case SessionLow:
			return ClaudeFirstDecision{Reason: "session_low"}
		}
		if WeeklyBandOf(be, now, th) == BandExhausted {
			return ClaudeFirstDecision{Reason: "exhausted"}
		}
		return ClaudeFirstDecision{OK: true, Headroom: claudeHeadroom(be)}
	}
	return ClaudeFirstDecision{Reason: "absent"}
}

// claudeHeadroom is the figure the start result cites: weekly remaining
// when published, else session remaining, else nil (unquantified).
func claudeHeadroom(be Backend) *float64 {
	if w, ok := be.Window(WindowWeekly); ok && w.RemainingPercent != nil {
		r := *w.RemainingPercent
		return &r
	}
	if w, ok := be.PrimaryAllowanceWindow(); ok && w.RemainingPercent != nil {
		r := *w.RemainingPercent
		return &r
	}
	if w, ok := be.Window(WindowSession); ok && w.RemainingPercent != nil {
		r := *w.RemainingPercent
		return &r
	}
	return nil
}
