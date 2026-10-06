// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"context"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// Claudia Resolve ranks published dests by plan slack; PreferProvider
// wins among token-eligible dests (🎯T691) when a caller supplies one, but
// an omit-provider mint (ResolveMint) no longer supplies Claude as that
// preference (🎯T1013.6 removed the 🎯T561 / 🎯T583 owner-economics rule
// outright) — see TestT1013_6ResolveMintHasNoClaudeBias below.

func TestT614ResolveSkipsHotClaude(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	week := now.Add(3*24*time.Hour + 12*time.Hour)
	lim := DefaultWeeklyWindowSeconds
	pct := func(v float64) *float64 { return &v }
	cands := []DestCand{
		{Provider: "claude", Backend: Backend{
			Provider: "claude", Status: StatusAvailable,
			Windows: []Window{{
				Name: WindowWeekly, RemainingPercent: pct(20), UsedPercent: pct(80),
				ResetsAt: &week, LimitWindowSeconds: &lim,
			}},
		}},
		{Provider: "grok", Backend: Backend{
			Provider: "grok", Status: StatusUnavailable, Reason: "unpublished",
		}},
	}
	got, err := claudia.Resolve(context.Background(), claudia.ModelPredicates{
		Mode:           claudia.CapabilitySession,
		PreferPlan:     true,
		PreferProvider: claudia.ProviderClaude,
		Now:            now,
		Usage:          backendsToPlanUsage(cands),
	})
	// With Claude hot and Grok unpublished there is no green published
	// dest: Resolve refuses (a named tie among unpublished catalog rows)
	// rather than landing on hot Claude. A refusal is the accepted answer;
	// a pick must not be Claude.
	if err == nil && got.Provider == claudia.ProviderClaude {
		t.Fatalf("hot Claude must not be chosen: %+v", got)
	}
}

// TestT1013_6ResolveMintHasNoClaudeBias is the concrete before/after the
// owner asked for (🎯T1013.6): Claude published with real headroom (50%
// weekly) alongside a fresher Grok (87%) and no explicit/portfolio/owner
// preference. Before this change ResolveMint passed PreferProvider=Claude
// into claudia.Resolve, which — per pickCatalog — restricts the candidate
// pool to the preferred provider outright whenever it is eligible (not
// merely a tie-break), so the mint landed on Claude despite Grok's larger
// slack. After removal ResolveMint passes no preference at all, so the
// pick is decided purely by claudia.Resolve's dest-band ranking (🎯T693:
// band first, slack within a band) — both providers are "ok" band here,
// so the one with more remaining headroom (Grok) wins.
func TestT1013_6ResolveMintHasNoClaudeBias(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	week := now.Add(3*24*time.Hour + 12*time.Hour)
	lim := DefaultWeeklyWindowSeconds
	pct := func(v float64) *float64 { return &v }
	cands := []DestCand{
		{Provider: "claude", Backend: Backend{
			Provider: "claude", Status: StatusAvailable,
			Windows: []Window{{
				Name: WindowWeekly, RemainingPercent: pct(50), UsedPercent: pct(50),
				ResetsAt: &week, LimitWindowSeconds: &lim,
			}},
		}},
		{Provider: "grok", Backend: Backend{
			Provider: "grok", Status: StatusAvailable,
			Windows: []Window{{
				Name: WindowWeekly, RemainingPercent: pct(87), UsedPercent: pct(13),
				ResetsAt: &week, LimitWindowSeconds: &lim,
			}},
		}},
	}
	// AFTER: ResolveMint (the production omit-provider path) picks the
	// greener Grok, not Claude — there is no Claude bias left to land on.
	got, err := ResolveMint(context.Background(), cands, now, DefaultThresholds())
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != claudia.ProviderGrok {
		t.Fatalf("ResolveMint with no bias = %+v, want grok (more headroom, same band)", got)
	}
	// BEFORE, for contrast: calling claudia.Resolve directly with the old
	// hardcoded PreferProvider=Claude landed on Claude despite Grok's
	// larger slack — this is the exact behavior ResolveMint no longer
	// reproduces.
	before, err := claudia.Resolve(context.Background(), claudia.ModelPredicates{
		Mode:           claudia.CapabilitySession,
		PreferPlan:     true,
		PreferProvider: claudia.ProviderClaude,
		Now:            now,
		Usage:          backendsToPlanUsage(cands),
	})
	if err != nil {
		t.Fatal(err)
	}
	if before.Provider != claudia.ProviderClaude {
		t.Fatalf("old PreferProvider=Claude shape = %+v, want claude (documenting the removed behavior)", before)
	}
}
