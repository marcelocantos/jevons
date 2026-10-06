// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/cli"

	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/planusage"
)

// t583Server is a mint-only server: registry, config default grok, and a
// plan feed under test. (Helper name is historical — 🎯T583 named the
// incident this harness was built to reproduce; the owner-economics
// "Claude first while it has headroom" rule it exercised was removed
// outright by 🎯T1013.6, not relocated. The harness itself stays useful
// for every other omit-provider mint test in this package.)
func t583Server(t *testing.T, backends func(now time.Time) []planusage.Backend, now time.Time) *Server {
	t.Helper()
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.SetDefaultProvider(string(claudia.ProviderGrok))
	// The leftover routing seed that named grok for code_implement.
	s.SetLLMPortfolioSource(&cost.Portfolio{
		DefaultProvider: cost.HarnessGrok,
		Routes: map[string]cost.TaskRoute{
			cost.TaskCodeImplement: {Prefer: []string{cost.HarnessGrok}},
		},
	}, true)
	s.SetPlanUsageSource(func() planusage.Snapshot {
		return planusage.Snapshot{At: now, Backends: backends(now)}
	})
	return s
}

func t583Weekly(provider string, remaining float64, now time.Time) planusage.Backend {
	used := 100 - remaining
	reset := now.Add(3 * 24 * time.Hour)
	lim := planusage.DefaultWeeklyWindowSeconds
	return planusage.Backend{
		Provider: provider, Status: planusage.StatusAvailable, FetchedAt: now,
		Windows: []planusage.Window{{
			Name: planusage.WindowWeekly, RemainingPercent: &remaining, UsedPercent: &used,
			ResetsAt: &reset, LimitWindowSeconds: &lim,
		}},
	}
}

// 🎯T1013.6 concrete before/after: a PO mint with provider omitted, config
// grok, a leftover portfolio file naming grok for code_implement, Grok
// weekly 87%, and Claude weekly 56% — the exact incident shape 🎯T583 once
// tested, except the owner rule it exercised is gone. Both providers are
// in the "ok" dest band, so the greener one (Grok, more remaining %) wins
// on slack alone; there is no Claude-specific step ahead of that ranking
// any more. Before this change (🎯T583 in place) this same fixture minted
// claude — see TestT1013_6ResolveMintHasNoClaudeBias's companion
// assertion in internal/planusage/t614_resolve_test.go for the
// side-by-side old-vs-new claudia.Resolve call.
func TestT1013_6OmitProviderMintHasNoClaudeBias(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	s := t583Server(t, func(now time.Time) []planusage.Backend {
		return []planusage.Backend{
			t583Weekly("grok", 87, now),
			t583Weekly("claude", 56, now),
		}
	}, now)
	def, _, note, err := s.stitchAgentStart(
		"jv-t1013-6-tape1", t.TempDir(), "", "", "",
		"jevons-po", claudia.PurposeWork, "", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if cli.PlanProvider(def.Provider) != claudia.ProviderGrok {
		t.Fatalf("omit-provider mint = %q, want grok (greener, no Claude bias left to override it)", def.Provider)
	}
	if !strings.Contains(note, "provider_knob: claudia") {
		t.Fatalf("note missing claudia citation: %q", note)
	}
	if strings.Contains(note, "claude-first") {
		t.Fatalf("claude-first knob must not exist any more: %q", note)
	}
	msg := formatAgentStartResult("jv-t1013-6-tape1", "/tmp/w", "jevons-po", "work", "worker", "", string(def.Provider), "", "sess", note, "")
	if !strings.Contains(msg, "provider_knob: claudia") {
		t.Fatalf("start result missing citation: %q", msg)
	}
}

// Claude at 0% (or 429) is exhausted — the mint falls back to a green
// provider and says which knob decided, rather than stalling the fleet
// on an empty plan. (Historically 🎯T583 tape 2; the claude-first knob it
// guarded against reappearing no longer exists, so the fallback-to-green
// behavior is the whole test now.)
func TestExhaustedClaudeFallsBackToGreenDest(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		claude planusage.Backend
	}{
		{"weekly zero", t583Weekly("claude", 0, now)},
		{"rate limited", planusage.Backend{Provider: "claude", Status: planusage.StatusUnavailable, Reason: "429 rate_limit", FetchedAt: now}},
	} {
		s := t583Server(t, func(now time.Time) []planusage.Backend {
			return []planusage.Backend{t583Weekly("grok", 87, now), tc.claude}
		}, now)
		def, _, note, err := s.stitchAgentStart(
			"jv-t583-"+strings.ReplaceAll(tc.name, " ", "-"), t.TempDir(), "", "", "",
			"jevons-po", claudia.PurposeWork, "", "",
		)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if cli.PlanProvider(def.Provider) != claudia.ProviderGrok {
			t.Fatalf("%s: fallback = %q, want grok", tc.name, def.Provider)
		}
		if strings.Contains(note, "claude-first") {
			t.Fatalf("%s: claude-first knob must not exist any more: %q", tc.name, note)
		}
		if !strings.Contains(note, "provider_knob: ") {
			t.Fatalf("%s: fallback note cites no knob: %q", tc.name, note)
		}
	}
}

// An explicit provider= still wins over the plan feed, Claude or not.
func TestExplicitProviderStillWinsOmitProviderMint(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	s := t583Server(t, func(now time.Time) []planusage.Backend {
		return []planusage.Backend{t583Weekly("grok", 87, now), t583Weekly("claude", 56, now)}
	}, now)
	def, _, note, err := s.stitchAgentStart(
		"jv-t583-explicit", t.TempDir(), "", "claude", "",
		"jevons-po", claudia.PurposeWork, "", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if def.Provider != claudia.ProviderClaude {
		t.Fatalf("explicit claude lost: %q", def.Provider)
	}
	if !strings.Contains(note, "provider_knob: explicit") {
		t.Fatalf("note = %q", note)
	}
}

// A resumed seat keeps its stored provider regardless of what the plan
// feed would now pick for an omit-provider mint.
func TestResumeKeepsStoredProviderOverPlanFeed(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	s := t583Server(t, func(now time.Time) []planusage.Backend {
		return []planusage.Backend{t583Weekly("grok", 87, now), t583Weekly("claude", 56, now)}
	}, now)
	wd := t.TempDir()
	if _, _, _, err := s.stitchAgentStart("jv-t583-resume", wd, "", "claude", "", "jevons-po", claudia.PurposeWork, "", ""); err != nil {
		t.Fatal(err)
	}
	def, existed, note, err := s.stitchAgentStart("jv-t583-resume", wd, "", "", "", "jevons-po", claudia.PurposeWork, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !existed || def.Provider != claudia.ProviderClaude {
		t.Fatalf("resume moved the seat: existed=%v provider=%q note=%q", existed, def.Provider, note)
	}
}

// 🎯T1013.6: every task class — including the T325.2.1 fast-cheap ones —
// mints the same greener dest (Grok here) now that there is no
// Claude-specific knob ahead of the plan-feed/claudia-resolve ranking.
// Before this change (🎯T583 in place) every one of these task types
// minted claude despite Grok having more headroom.
func TestT1013_6EveryTaskTypeUsesSamePlanRankingNoClaudeBias(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	for _, tt := range []string{cost.TaskCodeImplement, cost.TaskCEO, cost.TaskMechanical, cost.TaskOpsClassify, cost.TaskDesignProse, cost.TaskIdeation} {
		s := t583Server(t, func(now time.Time) []planusage.Backend {
			return []planusage.Backend{t583Weekly("grok", 87, now), t583Weekly("claude", 56, now)}
		}, now)
		def, _, note, err := s.stitchAgentStart(
			"jv-t583-"+tt, t.TempDir(), "", "", tt,
			"jevons-po", claudia.PurposeWork, "", "",
		)
		if err != nil {
			t.Fatalf("%s: %v", tt, err)
		}
		if cli.PlanProvider(def.Provider) != claudia.ProviderGrok {
			t.Fatalf("task_type %s minted %q, want grok (no Claude bias; note %q)", tt, def.Provider, note)
		}
		if strings.Contains(note, "claude-first") {
			t.Fatalf("task_type %s: claude-first knob must not exist any more: %q", tt, note)
		}
	}
}
