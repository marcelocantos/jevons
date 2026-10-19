// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T877: hot-provider seats reported under sidecar runtime ids
// (xai-oauth / anthropic / openai-codex) match their plan-usage rows,
// migrate to an eligible dest without treating a default-provider
// preference as a ban, and keep an explicit owner prohibition distinct
// from that preference. Live once-only move evidence is on 🎯T691 (J34 +
// development Grok→Claude sweep); this file pins the matching and
// constraint clauses hermetically.

func t877Weekly(provider string, rem, used float64, now time.Time) Backend {
	week := now.Add(3*24*time.Hour + 12*time.Hour)
	lim := DefaultWeeklyWindowSeconds
	return Backend{
		Provider: provider, Status: StatusAvailable,
		Windows: []Window{{
			Name: WindowWeekly, RemainingPercent: &rem, UsedPercent: &used,
			ResetsAt: &week, LimitWindowSeconds: &lim,
		}},
	}
}

// Observed 2026-09-27 shape: Grok weekly hot, Codex ahead, Claude eligible.
// A seat still registered as xai-oauth (OMP runtime id) must see the Grok
// plan row and get a Claude migrate — not a silent skip because the string
// "xai-oauth" never equals "grok".
func TestT877SidecarSeatMatchesPlanAndMigratesWhenGrokHot(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	grok := t877Weekly("grok", 20, 80, now)
	codex := t877Weekly("codex", 30, 70, now)
	claude := t877Weekly("claude", 80, 20, now)
	th := DefaultThresholds()
	if !MigrateOff(grok, now, th) {
		t.Fatal("fixture: grok must vacate (hot)")
	}
	if MigrateOff(codex, now, th) {
		t.Fatal("fixture: codex ahead must not vacate")
	}
	if MintIneligible(codex, now, th) != true {
		t.Fatal("fixture: codex ahead must be mint-ineligible")
	}
	if !DestEligible(claude, now, th) {
		t.Fatal("fixture: claude must be an eligible dest")
	}
	snap := Snapshot{At: now, Backends: []Backend{grok, codex, claude}}

	for _, seatID := range []string{"xai-oauth", "grok"} {
		acts := PlanActions(snap, []AgentRef{
			{Name: "jv-hot", Provider: seatID, Purpose: "work"},
		}, now, th)
		if len(acts) != 1 || acts[0].Action != SeatMigrate ||
			acts[0].From != "grok" || acts[0].To != "claude" ||
			acts[0].Author != destAuthor {
			t.Fatalf("seat %q must migrate grok→claude once: %+v", seatID, acts)
		}
	}

	// anthropic / openai-codex map the same way when their own plans vacate.
	claudeHot := t877Weekly("claude", 20, 80, now)
	anthropicSnap := Snapshot{At: now, Backends: []Backend{claudeHot, grok, t877Weekly("codex", 80, 20, now)}}
	acts := PlanActions(anthropicSnap, []AgentRef{
		{Name: "jv-claude-seat", Provider: "anthropic", Purpose: "work"},
	}, now, th)
	if len(acts) != 1 || acts[0].From != "claude" || acts[0].To == "" || acts[0].To == "claude" {
		t.Fatalf("anthropic seat must vacate the Claude plan onto another dest: %+v", acts)
	}

	codexHot := t877Weekly("codex", 20, 80, now)
	codexSnap := Snapshot{At: now, Backends: []Backend{codexHot, t877Weekly("claude", 80, 20, now), t877Weekly("grok", 55, 45, now)}}
	acts = PlanActions(codexSnap, []AgentRef{
		{Name: "jv-codex-seat", Provider: "openai-codex", Purpose: "work"},
	}, now, th)
	if len(acts) != 1 || acts[0].From != "codex" || acts[0].To == "" || acts[0].To == "codex" {
		t.Fatalf("openai-codex seat must vacate the Codex plan onto another dest: %+v", acts)
	}
}

// PreferProvider is a soft preference, not a ban on every other provider.
// A Grok preference on a seat whose own provider is hot still yields the
// only eligible dest (Claude); an explicit ExcludeProviders=[claude] is
// the prohibition and must not land on Claude.
func TestT877PreferIsNotBanAndExcludeIsProhibition(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	snap := Snapshot{At: now, Backends: []Backend{
		t877Weekly("grok", 20, 80, now),
		t877Weekly("codex", 30, 70, now), // ahead — not a dest
		t877Weekly("claude", 80, 20, now),
	}}
	th := DefaultThresholds()

	// Config/default-shaped preference for the hot provider itself must not
	// park the seat or invent a stay on Grok.
	preferGrok := PlanActions(snap, []AgentRef{{
		Name: "jv-pref", Provider: "xai-oauth", Purpose: "work",
		PreferProvider: claudia.ProviderGrok,
	}}, now, th)
	if len(preferGrok) != 1 || preferGrok[0].Action != SeatMigrate || preferGrok[0].To != "claude" {
		t.Fatalf("PreferProvider=grok must still migrate hot xai-oauth → claude: %+v", preferGrok)
	}

	// Owner prohibition on Claude is a distinct constraint: with Codex
	// ahead (not a dest) and Claude excluded, there is no eligible dest →
	// park with a reason, not a silent Claude land.
	banned := PlanActions(snap, []AgentRef{{
		Name: "jv-ban", Provider: "xai-oauth", Purpose: "work",
		PreferProvider:   claudia.ProviderClaude,
		ExcludeProviders: []claudia.Provider{claudia.ProviderClaude},
	}}, now, th)
	if len(banned) != 1 || banned[0].Action != SeatPark || banned[0].To != "" {
		t.Fatalf("ExcludeProviders=[claude] must park, not migrate to claude: %+v", banned)
	}
	if banned[0].Reason == "" || banned[0].Author != destAuthor {
		t.Fatalf("park must name Claudia and a reason: %+v", banned[0])
	}

	// Incomplete feed is an observable defer, not a repeated silent park.
	incomplete := Snapshot{At: now, Backends: []Backend{t877Weekly("grok", 20, 80, now)}}
	acts := PlanActions(incomplete, []AgentRef{{
		Name: "jv-alone", Provider: "xai-oauth", Purpose: "work",
	}}, now, th)
	if len(acts) != 0 {
		t.Fatalf("incomplete dest feed must not emit migrate/park: %+v", acts)
	}
	decisions := PlanDecisions(incomplete, []AgentRef{{
		Name: "jv-alone", Provider: "xai-oauth", Purpose: "work",
	}}, now, th)
	if len(decisions) != 1 || decisions[0].Action != SeatDefer || decisions[0].Reason == "" {
		t.Fatalf("incomplete feed must defer with a reason: %+v", decisions)
	}
}
