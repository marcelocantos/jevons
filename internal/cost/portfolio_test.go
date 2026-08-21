// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultPortfolioSeedTable(t *testing.T) {
	p := DefaultPortfolio()
	// Seed must cover design-map §5.2 task types.
	for _, tt := range []string{
		TaskCEO, TaskCodeImplement, TaskMechanical, TaskDesignProse,
		TaskOpsClassify, TaskJourneyGrok, TaskIdeation,
	} {
		if _, ok := p.Routes[tt]; !ok {
			t.Fatalf("missing route for task type %q", tt)
		}
	}
	// Multi-provider: code implement prefers non-default strong models first.
	code := p.Routes[TaskCodeImplement]
	if len(code.Prefer) < 1 || code.Prefer[0] == HarnessGrok {
		t.Fatalf("code_implement should prefer strong code harness first, got %+v", code.Prefer)
	}
	// Soft caps present for the three seed harnesses.
	for _, h := range []string{HarnessGrok, HarnessClaude, HarnessCodex} {
		if p.SoftCaps[h] <= 0 {
			t.Fatalf("soft cap for %s must be positive, got %d", h, p.SoftCaps[h])
		}
	}
}

func TestRoutePrefersFitWhenUnderCap(t *testing.T) {
	p := DefaultPortfolio()
	// Empty load: code implement → claude (first prefer).
	d := p.Route(TaskCodeImplement, nil)
	if d.Provider != HarnessClaude {
		t.Fatalf("got provider %q want claude; decision=%+v", d.Provider, d)
	}
	if d.TaskType != TaskCodeImplement {
		t.Fatalf("task_type=%q", d.TaskType)
	}
	if d.Reason != "fit" {
		t.Fatalf("reason=%q want fit", d.Reason)
	}
	if d.CapHit {
		t.Fatal("cap_hit should be false on empty load")
	}

	// CEO stays on daily harness.
	d = p.Route(TaskCEO, LoadCounts{})
	if d.Provider != HarnessGrok {
		t.Fatalf("ceo → %q want grok", d.Provider)
	}
}

func TestRouteUnderUtilisedWhenPreferredAtSoftCap(t *testing.T) {
	p := DefaultPortfolio()
	// Fill claude to soft cap; code_implement should spread to codex (next prefer).
	capClaude := p.SoftCaps[HarnessClaude]
	load := LoadCounts{HarnessClaude: capClaude}
	d := p.Route(TaskCodeImplement, load)
	if d.Provider != HarnessCodex {
		t.Fatalf("got %q want codex after claude at cap; decision=%+v", d.Provider, d)
	}
	if !d.CapHit {
		t.Fatal("expected CapHit when first prefer is full")
	}
	if d.Reason != "under_utilised" && d.Reason != "soft_cap_spread" {
		t.Fatalf("reason=%q", d.Reason)
	}

	// Fill both prefer; secondary grok should win (soft_cap_spread).
	load = LoadCounts{
		HarnessClaude: p.SoftCaps[HarnessClaude],
		HarnessCodex:  p.SoftCaps[HarnessCodex],
	}
	d = p.Route(TaskCodeImplement, load)
	if d.Provider != HarnessGrok {
		t.Fatalf("got %q want grok secondary; decision=%+v", d.Provider, d)
	}
	if d.Reason != "soft_cap_spread" {
		t.Fatalf("reason=%q want soft_cap_spread", d.Reason)
	}
}

func TestRouteDoesNotUseUSD(t *testing.T) {
	// Oracle: routing API takes LoadCounts (sessions) only — no BudgetConfig
	// USD fields. Subscription honesty stays in T137 monitor/enforcer.
	p := DefaultPortfolio()
	// Absurd "load" that would look like $ burn if miswired: only session keys.
	d := p.Route(TaskMechanical, LoadCounts{HarnessGrok: 0})
	if d.Provider != HarnessGrok {
		t.Fatalf("mechanical → %q", d.Provider)
	}
	// Soft-cap merge never touches Accounting / Limits.
	p2 := p.MergeSoftCaps(map[string]int{HarnessGrok: 2})
	if p2.SoftCaps[HarnessGrok] != 2 {
		t.Fatalf("soft cap overlay failed: %v", p2.SoftCaps)
	}
	// Original unchanged.
	if p.SoftCaps[HarnessGrok] == 2 && DefaultPortfolio().SoftCaps[HarnessGrok] != 2 {
		// clone must not mutate default seed soft caps on the returned overlay only.
	}
	if DefaultPortfolio().SoftCaps[HarnessGrok] != 12 {
		t.Fatalf("DefaultPortfolio soft cap mutated: %d", DefaultPortfolio().SoftCaps[HarnessGrok])
	}
}

func TestNormalizeTaskTypeAndPurpose(t *testing.T) {
	if NormalizeTaskType("") != TaskCodeImplement {
		t.Fatal("empty → code_implement")
	}
	if NormalizeTaskType("oracle") != TaskMechanical {
		t.Fatal("oracle alias")
	}
	if NormalizeTaskType("aside") != TaskIdeation {
		t.Fatal("aside alias")
	}
	if TaskTypeFromPurpose("overseer") != TaskCEO {
		t.Fatal("purpose overseer")
	}
	if TaskTypeFromPurpose("aside") != TaskIdeation {
		t.Fatal("purpose aside")
	}
	if TaskTypeFromPurpose("work") != TaskCodeImplement {
		t.Fatal("purpose work")
	}
	if NormalizeTaskType("nudge") != TaskMechanical {
		t.Fatal("nudge alias")
	}
	if NormalizeTaskType("ack") != TaskMechanical {
		t.Fatal("ack alias")
	}
}

func TestLoadPortfolioFileMissingAndMalformed(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope.json")
	p, err := LoadPortfolioFile(missing)
	if err != nil {
		t.Fatal(err)
	}
	if p.DefaultProvider != HarnessGrok {
		t.Fatalf("missing file default=%q", p.DefaultProvider)
	}

	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPortfolioFile(bad); err == nil {
		t.Fatal("malformed must error")
	}

	good := filepath.Join(dir, "good.json")
	if err := os.WriteFile(good, []byte(`{
  "default_provider": "claude",
  "soft_caps": {"claude": 3},
  "routes": {
    "code_implement": {"prefer": ["claude"], "secondary": ["grok"]}
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err = LoadPortfolioFile(good)
	if err != nil {
		t.Fatal(err)
	}
	if p.DefaultProvider != "claude" || p.SoftCaps["claude"] != 3 {
		t.Fatalf("loaded: %+v soft=%v", p.DefaultProvider, p.SoftCaps)
	}
	d := p.Route(TaskCodeImplement, LoadCounts{"claude": 3})
	if d.Provider != HarnessGrok {
		t.Fatalf("after soft cap, want grok secondary got %q", d.Provider)
	}
}

// Soft caps respect subscription-era honesty: session spread works even
// when accounting=subscription (no USD path in Route).
func TestPortfolioSpreadIndependentOfAccountingMode(t *testing.T) {
	cfg := DefaultBudgetConfig()
	cfg.Accounting = AccountingSubscription
	if !cfg.IsSubscription() {
		t.Fatal("fixture")
	}
	// Route ignores cfg entirely — only LoadCounts matter.
	p := DefaultPortfolio()
	d := p.Route(TaskCodeImplement, LoadCounts{HarnessClaude: p.SoftCaps[HarnessClaude]})
	if d.Provider == HarnessClaude {
		t.Fatal("should spread off capped claude regardless of subscription accounting")
	}
}

// A drained subscription is steered from the override file, not a rebuild:
// every work route moves to one provider, its cap is lifted to 0 so a large
// fleet never spreads back onto the drained harness, and the route's model
// pin reaches the decision. Regression net for the owner's llm-portfolio.json
// after Grok ran out of tokens (2026-08-09).
func TestOverrideFileStrandsDrainedProvider(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "llm-portfolio.json")
	if err := os.WriteFile(path, []byte(`{
  "default_provider": "claude",
  "soft_caps": {"claude": 0},
  "routes": {
    "ceo":            {"prefer": ["claude"], "model": "claude-opus-5"},
    "code_implement": {"prefer": ["claude"], "model": "claude-opus-5"},
    "mechanical":     {"prefer": ["claude"], "model": "claude-opus-5"},
    "design_prose":   {"prefer": ["claude"], "model": "claude-opus-5"},
    "ops_classify":   {"prefer": ["claude"], "model": "claude-opus-5"},
    "ideation":       {"prefer": ["claude"], "model": "claude-opus-5"}
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := LoadPortfolioFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Far past the compiled claude cap of 8: a lifted cap must not spread.
	load := LoadCounts{HarnessClaude: 40}
	for _, tt := range []string{
		TaskCEO, TaskCodeImplement, TaskMechanical,
		TaskDesignProse, TaskOpsClassify, TaskIdeation,
	} {
		d := p.Route(tt, load)
		if d.Provider != HarnessClaude {
			t.Errorf("%s routed to %q, want claude", tt, d.Provider)
		}
		if d.Model != "claude-opus-5" {
			t.Errorf("%s model pin %q, want claude-opus-5", tt, d.Model)
		}
	}
	// The provider-under-test oracle path is untouched by the override.
	if d := p.Route(TaskJourneyGrok, load); d.Provider != HarnessGrok {
		t.Errorf("journey_grok routed to %q, want grok", d.Provider)
	}
}
