// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T824: a window whose resets_at is already past must not empty the dest
// band while headroom is still visible on the reading.
func TestT824ResolveMintTreatsRolledWindowAsEligible(t *testing.T) {
	now := time.Date(2026, 9, 22, 7, 19, 50, 0, time.UTC)
	sessionReset := now.Add(-40 * time.Minute) // rolled while fetch is fresh
	sessLim := DefaultSessionWindowSeconds
	pct := func(v float64) *float64 { return &v }
	claude := Backend{
		Provider: "claude", Status: StatusAvailable, Stale: true,
		FetchedAt: now.Add(-30 * time.Second),
		Windows: []Window{
			{
				Name: WindowSession, RemainingPercent: pct(50), UsedPercent: pct(50),
				ResetsAt: &sessionReset, LimitWindowSeconds: &sessLim,
			},
			bandWindow(WindowWeekly, 30, 70, now, 0.5),
		},
	}
	if !DestEligible(claude, now, DefaultThresholds()) {
		t.Fatal("rolled session + headroom must stay dest-eligible")
	}
	pick, err := ResolveMint(context.Background(), []DestCand{
		{Provider: "claude", Backend: claude},
	}, now, DefaultThresholds(), nil)
	if err != nil || pick.Provider != claudia.ProviderClaude {
		t.Fatalf("ResolveMint = %+v err=%v, want claude", pick, err)
	}
}

// 🎯T824: when fetch-age staleness is the only blocker, refusal names it.
func TestT824MintRefusalNamesStaleFetchAge(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	week := now.Add(3 * 24 * time.Hour)
	lim := DefaultWeeklyWindowSeconds
	pct := func(v float64) *float64 { return &v }
	be := Backend{
		Provider: "claude", Status: StatusAvailable, Stale: true, AgeSeconds: 20 * 60,
		FetchedAt: now.Add(-20 * time.Minute),
		Windows: []Window{{
			Name: WindowWeekly, RemainingPercent: pct(40), UsedPercent: pct(60),
			ResetsAt: &week, LimitWindowSeconds: &lim,
		}},
	}
	got := MintRefusalDetail([]DestCand{{Provider: "claude", Backend: be}}, now, DefaultThresholds())
	want := "plan reading stale (fetched 20m ago)"
	if !strings.Contains(got, want) {
		t.Fatalf("refusal %q lacks %q", got, want)
	}
}

// 🎯T824: migrate/park copy names the weekly band that excluded the provider.
func TestT824MigrateOffReasonNamesWeeklyHot(t *testing.T) {
	now := time.Date(2026, 9, 22, 7, 19, 50, 0, time.UTC)
	be := Backend{
		Provider: "claude", Status: StatusAvailable,
		Windows: []Window{bandWindow(WindowWeekly, 56, 44, now, 5.0/7.0)},
	}
	got := migrateOffReason(be, now, DefaultThresholds())
	if !strings.Contains(got, "weekly ahead of pace (56% used)") && !strings.Contains(got, "weekly hot (56% used)") {
		t.Fatalf("migrateOffReason = %q, want a named weekly band with used percent", got)
	}
}
