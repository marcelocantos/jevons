// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"testing"
	"time"
)

func t583pf(v float64) *float64 { return &v }

func t583Backend(provider string, weeklyRemaining, sessionRemaining *float64, now time.Time) Backend {
	reset := now.Add(3 * 24 * time.Hour)
	lim := DefaultWeeklyWindowSeconds
	be := Backend{Provider: provider, Status: StatusAvailable, FetchedAt: now}
	if weeklyRemaining != nil {
		used := 100 - *weeklyRemaining
		be.Windows = append(be.Windows, Window{
			Name: WindowWeekly, RemainingPercent: weeklyRemaining, UsedPercent: &used,
			ResetsAt: &reset, LimitWindowSeconds: &lim,
		})
	}
	if sessionRemaining != nil {
		used := 100 - *sessionRemaining
		sreset := now.Add(2 * time.Hour)
		be.Windows = append(be.Windows, Window{
			Name: WindowSession, RemainingPercent: sessionRemaining, UsedPercent: &used,
			ResetsAt: &sreset,
		})
	}
	return be
}

// 🎯T583 tape 1: Claude weekly 56% with a fresher Grok alongside — the
// owner rule says Claude, and the headroom figure is the weekly one.
func TestT583ClaudeWithHeadroomIsEligible(t *testing.T) {
	now := time.Now()
	th := DefaultThresholds()
	cands := []DestCand{
		{Provider: "grok", Backend: t583Backend("grok", t583pf(95), t583pf(99), now)},
		{Provider: "claude", Backend: t583Backend("claude", t583pf(56), t583pf(80), now)},
	}
	d := ClaudeFirst(cands, now, th)
	if !d.OK || d.Reason != "" {
		t.Fatalf("claude with headroom: %+v", d)
	}
	if d.Headroom == nil || *d.Headroom != 56 {
		t.Fatalf("headroom = %v, want 56", d.Headroom)
	}
}

// 🎯T583 tape 2: a 0% weekly Claude, a 429 Claude, and a signed-out Claude
// are each ineligible — the mint must fall back, not sit on an empty plan.
func TestT583ExhaustedOrBlockedClaudeIsNotEligible(t *testing.T) {
	now := time.Now()
	th := DefaultThresholds()
	for _, tc := range []struct {
		name   string
		be     Backend
		reason string
	}{
		{"weekly zero", t583Backend("claude", t583pf(0), t583pf(80), now), "exhausted"},
		{"session zero", t583Backend("claude", t583pf(60), t583pf(0), now), "exhausted"},
		// 🎯T677: still ineligible, but for the honest reason — we could
		// not read it, which is "blocked", not a claim that it is spent.
		{"rate limited", Backend{Provider: "claude", Status: StatusUnavailable, Reason: "429 rate_limit", FetchedAt: now}, "blocked"},
		{"signed out", Backend{Provider: "claude", Status: StatusUnavailable, Reason: "not signed in", FetchedAt: now}, "blocked"},
	} {
		d := ClaudeFirst([]DestCand{{Provider: "claude", Backend: tc.be}}, now, th)
		if d.OK || d.Reason != tc.reason {
			t.Fatalf("%s: got %+v, want reason %q", tc.name, d, tc.reason)
		}
	}
}

// 🎯T583: a session window at or under the low threshold empties mid-turn,
// so it is not headroom; and a feed with no Claude row is unknown, never
// an assumed yes.
func TestT583SessionLowAndAbsent(t *testing.T) {
	now := time.Now()
	th := DefaultThresholds()
	low := th.LowRemainingPercent
	d := ClaudeFirst([]DestCand{{Provider: "claude", Backend: t583Backend("claude", t583pf(60), &low, now)}}, now, th)
	if d.OK || d.Reason != "session_low" {
		t.Fatalf("session low: %+v", d)
	}
	d = ClaudeFirst([]DestCand{{Provider: "grok", Backend: t583Backend("grok", t583pf(90), nil, now)}}, now, th)
	if d.OK || d.Reason != "absent" {
		t.Fatalf("absent claude: %+v", d)
	}
}

// 🎯T583: a published Claude that quantifies nothing is still eligible —
// the citation says unknown rather than inventing a percentage.
func TestT583UnquantifiedClaudeIsEligibleWithUnknownHeadroom(t *testing.T) {
	now := time.Now()
	th := DefaultThresholds()
	be := Backend{Provider: "claude", Status: StatusAvailable, FetchedAt: now,
		Windows: []Window{{Name: WindowWeekly}}}
	d := ClaudeFirst([]DestCand{{Provider: "claude", Backend: be}}, now, th)
	if !d.OK || d.Headroom != nil {
		t.Fatalf("unquantified claude: %+v", d)
	}
}
