// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"strings"
	"testing"
	"time"
)

// 🎯T390.1.5.1 hermetics: session remaining is a server eligibility veto.
// Session does not use leftover-vs-time ranking; only remaining % and 429.

func t390151pct(v float64) *float64 { return &v }

func t390151HealthyWeekly(provider string, now time.Time) Window {
	week := now.Add(3*24*time.Hour + 12*time.Hour) // 50% of 7d left
	lim := DefaultWeeklyWindowSeconds
	return Window{
		Name: WindowWeekly, RemainingPercent: t390151pct(50), UsedPercent: t390151pct(50),
		ResetsAt: &week, LimitWindowSeconds: &lim,
	}
}

func t390151Session(rem float64, now time.Time) Window {
	reset := now.Add(2 * time.Hour)
	lim := DefaultSessionWindowSeconds
	return Window{
		Name: WindowSession, RemainingPercent: t390151pct(rem), UsedPercent: t390151pct(100 - rem),
		ResetsAt: &reset, LimitWindowSeconds: &lim,
	}
}

func t390151Backend(provider string, sessionRem *float64, now time.Time) Backend {
	wins := []Window{t390151HealthyWeekly(provider, now)}
	if sessionRem != nil {
		wins = append([]Window{t390151Session(*sessionRem, now)}, wins...)
	}
	return Backend{Provider: provider, Status: StatusAvailable, Windows: wins, FetchedAt: now}
}

// Grok weekly healthy + session 0% → refuse Grok as dest (PickPlanDest /
// DestEligible). Session exhausted also migrates running seats.
func TestT390_1_5_1SessionZeroRefusesDest(t *testing.T) {
	th := DefaultThresholds()
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	zero := 0.0
	grok := t390151Backend("grok", &zero, now)
	claude := t390151Backend("claude", t390151pct(80), now)

	if WeeklyBandOf(grok, now, th) != BandOK {
		t.Fatalf("fixture: grok weekly must be healthy, got %s", WeeklyBandOf(grok, now, th))
	}
	if SessionStatusOf(grok, th) != SessionExhausted {
		t.Fatalf("fixture: grok session 0 → exhausted, got %s", SessionStatusOf(grok, th))
	}
	if DestEligible(grok, now, th) {
		t.Fatal("healthy weekly + session 0% must be dest-ineligible")
	}
	if !MigrateOff(grok, now, th) {
		t.Fatal("session 0% must migrate-off (same actuator as weekly hot)")
	}
	if !MintIneligible(grok, now, th) {
		t.Fatal("session 0% must be mint-ineligible")
	}

	got, ok := PickPlanDest([]DestCand{
		{Provider: "grok", Backend: grok, Load: 1},
		{Provider: "claude", Backend: claude, Load: 2},
	}, now, th)
	if !ok || got != "claude" {
		t.Fatalf("PickPlanDest must skip session-dead grok, got %q ok=%v", got, ok)
	}

	pick := PickMintDest([]DestCand{
		{Provider: "grok", Backend: grok, Load: 1},
		{Provider: "claude", Backend: claude, Load: 2},
	}, "grok", now, th)
	if !pick.OK || pick.Provider != "claude" {
		t.Fatalf("omit-provider mint must refuse grok session 0, got %+v", pick)
	}

	acts := PlanActions(Snapshot{Backends: []Backend{grok, claude}}, []AgentRef{
		{Name: "w", Provider: "grok", Purpose: "work"},
	}, now, th)
	if len(acts) != 1 || acts[0].To != "claude" {
		t.Fatalf("session exhausted migrates grok→claude: %+v", acts)
	}
	if !strings.Contains(acts[0].Reason, "session") {
		t.Fatalf("migrate reason should name session: %q", acts[0].Reason)
	}
}

// Session remaining-low (≤ LowRemainingPercent, > 0) refuses Grok as
// omit-provider default / dest, but does NOT bounce the fleet.
func TestT390_1_5_1SessionRemainingLowMintOnly(t *testing.T) {
	th := DefaultThresholds()
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	low := th.LowRemainingPercent // 15 — paints as remaining-low in the ticker
	grok := t390151Backend("grok", &low, now)
	claude := t390151Backend("claude", t390151pct(90), now)

	if WeeklyBandOf(grok, now, th) != BandOK {
		t.Fatalf("fixture: grok weekly healthy, got %s", WeeklyBandOf(grok, now, th))
	}
	if SessionStatusOf(grok, th) != SessionLow {
		t.Fatalf("fixture: session %g → low, got %s", low, SessionStatusOf(grok, th))
	}
	if DestEligible(grok, now, th) {
		t.Fatal("session remaining-low must be dest-ineligible")
	}
	if MigrateOff(grok, now, th) {
		t.Fatal("session remaining-low must NOT migrate-off (mint only)")
	}
	if !MintIneligible(grok, now, th) {
		t.Fatal("session remaining-low must be mint-ineligible")
	}

	got, ok := PickPlanDest([]DestCand{
		{Provider: "grok", Backend: grok, Load: 0},
		{Provider: "claude", Backend: claude, Load: 5},
	}, now, th)
	if !ok || got != "claude" {
		t.Fatalf("PickPlanDest skips remaining-low grok: dest=%q ok=%v", got, ok)
	}

	pick := PickMintDest([]DestCand{
		{Provider: "grok", Backend: grok, Load: 0},
		{Provider: "claude", Backend: claude, Load: 5},
	}, "grok", now, th)
	if !pick.OK || pick.Provider != "claude" {
		t.Fatalf("omit-provider default must refuse remaining-low grok, got %+v", pick)
	}

	acts := PlanActions(Snapshot{Backends: []Backend{grok, claude}}, []AgentRef{
		{Name: "w", Provider: "grok", Purpose: "work"},
	}, now, th)
	if len(acts) != 0 {
		t.Fatalf("remaining-low must not bounce fleet, got %+v", acts)
	}
}

// 429 / rate_limit on the backend reason is session-exhausted even when a
// weekly window would otherwise look fine before CockpitSnapshot rewrite.
func TestT390_1_5_1Session429Migrates(t *testing.T) {
	th := DefaultThresholds()
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	be := Backend{
		Provider: "claude",
		Status:   StatusUnavailable,
		Reason:   "Claude usage HTTP 429: rate_limit_error",
	}
	if SessionStatusOf(be, th) != SessionExhausted {
		t.Fatalf("429 → session exhausted, got %s", SessionStatusOf(be, th))
	}
	if !MigrateOff(be, now, th) {
		t.Fatal("429 must migrate-off")
	}
	if DestEligible(be, now, th) {
		t.Fatal("429 must not be dest-eligible")
	}
}

// No published session window is not a veto — weekly ranking alone decides.
func TestT390_1_5_1UnpublishedSessionNotVeto(t *testing.T) {
	th := DefaultThresholds()
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	grok := t390151Backend("grok", nil, now)
	if SessionStatusOf(grok, th) != SessionUnpublished {
		t.Fatalf("no session → unpublished, got %s", SessionStatusOf(grok, th))
	}
	if !DestEligible(grok, now, th) {
		t.Fatal("unpublished session must not veto a healthy weekly dest")
	}
	if MintIneligible(grok, now, th) || MigrateOff(grok, now, th) {
		t.Fatal("unpublished session is neither mint-ineligible nor migrate-off")
	}
}

