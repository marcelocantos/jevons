// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"context"
	"testing"
	"time"
)

// 🎯T691: dest ranking lives in claudia.Resolve. Jevons records the author.
func TestT691PlanActionsAuthorIsClaudia(t *testing.T) {
	th := DefaultThresholds()
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	week := now.Add(3*24*time.Hour + 12*time.Hour)
	lim := DefaultWeeklyWindowSeconds
	pct := func(v float64) *float64 { return &v }
	snap := Snapshot{Backends: []Backend{
		{
			Provider: "grok", Status: StatusAvailable,
			Windows: []Window{{
				Name: WindowWeekly, RemainingPercent: pct(0), UsedPercent: pct(100),
				ResetsAt: &week, LimitWindowSeconds: &lim,
			}},
		},
		{
			Provider: "claude", Status: StatusAvailable,
			Windows: []Window{{
				Name: WindowWeekly, RemainingPercent: pct(80), UsedPercent: pct(20),
				ResetsAt: &week, LimitWindowSeconds: &lim,
			}},
		},
	}}
	acts := PlanActions(snap, []AgentRef{
		{Name: "w", Provider: "grok", Purpose: "work"},
	}, now, th)
	if len(acts) != 1 || acts[0].To != "claude" {
		t.Fatalf("migrate grok → claude: %+v", acts)
	}
	if acts[0].Author != destAuthor {
		t.Fatalf("author = %q, want claudia", acts[0].Author)
	}
}

// 🎯T691: ahead-only + hot is a claudia refusal (no dest band), not a
// jevons veto in front of Resolve. RequireUsage fails closed.
func TestT691ResolveMintRefusesWhenNoDestBand(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	th := DefaultThresholds()
	cands := []DestCand{
		{Provider: "grok", Backend: t495Backend("grok", t495pf(85), t495pf(15), t495Week(0.5), now)},
	}
	_, err := ResolveMint(context.Background(), cands, now, th)
	if err == nil {
		t.Fatal("hot-only feed must fail closed through claudia, not invent a dest")
	}
}
