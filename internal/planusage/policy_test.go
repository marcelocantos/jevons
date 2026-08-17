// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"testing"
	"time"
)

func TestWeeklyBandTable(t *testing.T) {
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

	if got := WeeklyBandOf(weekly(45, 55), now, th); got != BandAhead {
		t.Fatalf("burn 1.1 → ahead, got %s", got)
	}
	if MintIneligible(weekly(45, 55), now, th) != true {
		t.Fatal("ahead is mint-ineligible")
	}
	if MigrateOff(weekly(45, 55), now, th) {
		t.Fatal("ahead is not migrate-off")
	}

	if got := WeeklyBandOf(weekly(20, 80), now, th); got != BandHot {
		t.Fatalf("burn 1.6 → hot, got %s", got)
	}
	if !MigrateOff(weekly(20, 80), now, th) {
		t.Fatal("hot migrates")
	}

	if got := WeeklyBandOf(weekly(0, 100), now, th); got != BandExhausted {
		t.Fatalf("0 remaining → exhausted, got %s", got)
	}

	if got := WeeklyBandOf(Backend{
		Provider: "claude", Status: StatusUnavailable,
		Reason: "Claude usage HTTP 429: rate_limit_error",
	}, now, th); got != BandExhausted {
		t.Fatalf("429 → exhausted, got %s", got)
	}

	// leftover 16% at 1:1 pace: used 42, rem 58, elapsed 50 → cont = 16
	if got := WeeklyBandOf(weekly(58, 42), now, th); got != BandUnder {
		t.Fatalf("continuation waste 16 → under, got %s", got)
	}
}

func TestPickPlanDestWasteThenLoad(t *testing.T) {
	th := DefaultThresholds()
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	week := now.Add(3*24*time.Hour + 12*time.Hour)
	lim := DefaultWeeklyWindowSeconds
	pct := func(v float64) *float64 { return &v }
	be := func(name string, rem, used float64) Backend {
		return Backend{
			Provider: name, Status: StatusAvailable,
			Windows: []Window{{
				Name: WindowWeekly, RemainingPercent: pct(rem), UsedPercent: pct(used),
				ResetsAt: &week, LimitWindowSeconds: &lim,
			}},
		}
	}
	// claude under (waste), grok ok with lower load — waste band wins.
	cands := []DestCand{
		{Provider: "claude", Backend: be("claude", 58, 42), Load: 5},
		{Provider: "grok", Backend: be("grok", 50, 50), Load: 1},
	}
	got, ok := PickPlanDest(cands, now, th)
	if !ok || got != "claude" {
		t.Fatalf("waste band beats load: dest=%q ok=%v", got, ok)
	}

	// two greens: least load (not highest remaining)
	cands = []DestCand{
		{Provider: "grok", Backend: be("grok", 50, 50), Load: 4},
		{Provider: "codex", Backend: be("codex", 55, 45), Load: 1},
	}
	got, ok = PickPlanDest(cands, now, th)
	if !ok || got != "codex" {
		t.Fatalf("least load among greens: dest=%q ok=%v", got, ok)
	}

	// all hot
	hot := be("grok", 20, 80)
	got, ok = PickPlanDest([]DestCand{{Provider: "grok", Backend: hot, Load: 2}}, now, th)
	if ok || got != "" {
		t.Fatalf("all-hot dest empty, got %q ok=%v", got, ok)
	}
}

func TestPlanActionsParkWhenNoDest(t *testing.T) {
	th := DefaultThresholds()
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	week := now.Add(3 * 24 * time.Hour)
	lim := DefaultWeeklyWindowSeconds
	zero, used := 0.0, 100.0
	snap := Snapshot{Backends: []Backend{{
		Provider: "grok", Status: StatusAvailable,
		Windows: []Window{{
			Name: WindowWeekly, RemainingPercent: &zero, UsedPercent: &used,
			ResetsAt: &week, LimitWindowSeconds: &lim,
		}},
	}}}
	acts := PlanActions(snap, []AgentRef{
		{Name: "jevons", Provider: "grok", Purpose: "overseer"},
		{Name: "worker", Provider: "grok", Purpose: "work"},
	}, now, th)
	if len(acts) != 1 || acts[0].Name != "worker" || acts[0].To != "" {
		t.Fatalf("overseer skipped; worker parks: %+v", acts)
	}
}

func TestPlanActionsMigrateToDest(t *testing.T) {
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
}
