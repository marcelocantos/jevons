// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"strings"
	"testing"
	"time"
)

// TestT285_2WeeklyBandDetail: the "!" tooltip and disabled menu rows carry
// a server-computed reason from the same band classification the sweep
// uses — never a client-side re-derivation.
func TestT285_2WeeklyBandDetail(t *testing.T) {
	th := DefaultThresholds()
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	week := now.Add(3*24*time.Hour + 12*time.Hour) // 50% of 7d remaining
	lim := DefaultWeeklyWindowSeconds

	pct := func(v float64) *float64 { return &v }
	weekly := func(rem, used float64) Backend {
		return Backend{
			Provider: "grok",
			Status:   StatusAvailable,
			Windows: []Window{{
				Name: WindowWeekly, RemainingPercent: pct(rem), UsedPercent: pct(used),
				ResetsAt: &week, LimitWindowSeconds: &lim,
			}},
		}
	}

	hot := WeeklyBandDetail(weekly(20, 80), now, th)
	if hot.Band != BandHot || hot.Eligible {
		t.Fatalf("hot: band=%s eligible=%v", hot.Band, hot.Eligible)
	}
	for _, want := range []string{"Grok", "hot", "burn", "20% remaining"} {
		if !strings.Contains(hot.Reason, want) {
			t.Fatalf("hot reason %q lacks %q", hot.Reason, want)
		}
	}

	ahead := WeeklyBandDetail(weekly(45, 55), now, th)
	if ahead.Band != BandAhead || ahead.Eligible {
		t.Fatalf("ahead: band=%s eligible=%v", ahead.Band, ahead.Eligible)
	}
	if !strings.Contains(ahead.Reason, "ahead of pace") {
		t.Fatalf("ahead reason = %q", ahead.Reason)
	}

	// On pace at 1:1 → ok and an eligible dest; no scary words.
	ok := WeeklyBandDetail(weekly(50, 50), now, th)
	if ok.Band != BandOK || !ok.Eligible {
		t.Fatalf("ok: band=%s eligible=%v (reason %q)", ok.Band, ok.Eligible, ok.Reason)
	}

	exhausted := WeeklyBandDetail(Backend{
		Provider: "claude", Status: StatusUnavailable,
		Reason: "Claude usage HTTP 429: rate_limit_error",
	}, now, th)
	if exhausted.Band != BandExhausted || exhausted.Eligible {
		t.Fatalf("exhausted: band=%s eligible=%v", exhausted.Band, exhausted.Eligible)
	}
	if !strings.Contains(exhausted.Reason, "rate-limited") {
		t.Fatalf("exhausted reason = %q", exhausted.Reason)
	}

	unpub := WeeklyBandDetail(Backend{Provider: "codex", Status: StatusUnavailable,
		Reason: "no plan-remaining published"}, now, th)
	if unpub.Band != BandUnpublished || unpub.Eligible {
		t.Fatalf("unpublished: band=%s eligible=%v", unpub.Band, unpub.Eligible)
	}
	if !strings.Contains(unpub.Reason, "no plan-remaining published") {
		t.Fatalf("unpublished reason = %q", unpub.Reason)
	}
}
