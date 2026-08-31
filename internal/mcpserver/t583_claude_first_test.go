// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/planusage"
)

// t583Server is a mint-only server: registry, config default grok (the
// setting that produced the incident), and a plan feed under test.
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

// 🎯T583 tape 1: the incident's own shape — a PO mint with provider omitted,
// config grok, a leftover portfolio file naming grok for code_implement, and
// Claude weekly 56%. The seat comes up claude and the note cites the knob.
func TestT583OmitProviderMintsClaudeWithHeadroom(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	s := t583Server(t, func(now time.Time) []planusage.Backend {
		return []planusage.Backend{
			t583Weekly("grok", 87, now),
			t583Weekly("claude", 56, now),
		}
	}, now)
	def, _, note, err := s.stitchAgentStart(
		"jv-t583-tape1", t.TempDir(), "", "", "",
		"jevons-po", claudia.PurposeWork, "", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if def.Provider != claudia.ProviderClaude {
		t.Fatalf("omit-provider mint = %q, want claude (grok 87%% is fresher but the owner rule is claude-first)", def.Provider)
	}
	if !strings.Contains(note, "provider_knob: claude-first: plan headroom 56%") {
		t.Fatalf("note missing claude-first citation: %q", note)
	}
	// The owner sees the same line in the tool result.
	msg := formatAgentStartResult("jv-t583-tape1", "/tmp/w", "jevons-po", "work", "worker", "", string(def.Provider), "", "sess", note, "")
	if !strings.Contains(msg, "claude-first: plan headroom 56%") {
		t.Fatalf("start result missing citation: %q", msg)
	}
}

// 🎯T583 tape 2: Claude at 0% (or 429) is exhausted — the mint falls back to
// a green provider and says which knob decided, rather than stalling the
// fleet on an empty plan.
func TestT583ExhaustedClaudeFallsBack(t *testing.T) {
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
		if def.Provider != claudia.ProviderGrok {
			t.Fatalf("%s: fallback = %q, want grok", tc.name, def.Provider)
		}
		if strings.Contains(note, "claude-first") {
			t.Fatalf("%s: exhausted Claude still cited claude-first: %q", tc.name, note)
		}
		if !strings.Contains(note, "provider_knob: ") {
			t.Fatalf("%s: fallback note cites no knob: %q", tc.name, note)
		}
	}
}

// 🎯T583 tape 3: an explicit provider= still wins over claude-first.
func TestT583ExplicitProviderStillWins(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	s := t583Server(t, func(now time.Time) []planusage.Backend {
		return []planusage.Backend{t583Weekly("grok", 87, now), t583Weekly("claude", 56, now)}
	}, now)
	def, _, note, err := s.stitchAgentStart(
		"jv-t583-explicit", t.TempDir(), "", "grok", "",
		"jevons-po", claudia.PurposeWork, "", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if def.Provider != claudia.ProviderGrok {
		t.Fatalf("explicit grok lost to claude-first: %q", def.Provider)
	}
	if !strings.Contains(note, "provider_knob: explicit") {
		t.Fatalf("note = %q", note)
	}
}

// 🎯T583: a resumed seat keeps its stored provider — claude-first is a mint
// knob, not a mid-flight reassignment.
func TestT583ResumeKeepsStoredProvider(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	s := t583Server(t, func(now time.Time) []planusage.Backend {
		return []planusage.Backend{t583Weekly("grok", 87, now), t583Weekly("claude", 56, now)}
	}, now)
	wd := t.TempDir()
	if _, _, _, err := s.stitchAgentStart("jv-t583-resume", wd, "", "grok", "", "jevons-po", claudia.PurposeWork, "", ""); err != nil {
		t.Fatal(err)
	}
	def, existed, note, err := s.stitchAgentStart("jv-t583-resume", wd, "", "", "", "jevons-po", claudia.PurposeWork, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !existed || def.Provider != claudia.ProviderGrok {
		t.Fatalf("resume moved the seat: existed=%v provider=%q note=%q", existed, def.Provider, note)
	}
}

// 🎯T583: every task class goes to Claude, including the T325.2.1 fast-cheap
// ones — and a Claude seat takes no grok/codex fast-cheap model pin.
func TestT583EveryTaskTypeMintsClaude(t *testing.T) {
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
		if def.Provider != claudia.ProviderClaude {
			t.Fatalf("task_type %s minted %q, want claude (note %q)", tt, def.Provider, note)
		}
		if m := strings.ToLower(def.Model); strings.Contains(m, "grok") || strings.Contains(m, "gpt") {
			t.Fatalf("task_type %s pinned a non-Claude model on a Claude seat: %q", tt, def.Model)
		}
	}
}
