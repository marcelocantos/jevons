// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 🎯T476: omit-provider mint follows config.yaml, not the compiled seed
// that still prefers Claude for code_implement.
func TestPickMintProviderOmitFollowsConfigNotCompiledSeed(t *testing.T) {
	seed := DefaultPortfolio()
	would := seed.Route(TaskCodeImplement, nil)
	if would.Provider != HarnessClaude {
		t.Fatalf("fixture: compiled seed should still prefer claude for code_implement, got %q", would.Provider)
	}
	pick := PickMintProvider(MintProviderArgs{
		ConfigProvider:    HarnessGrok,
		Portfolio:         would,
		PortfolioFromFile: false,
	})
	if pick.Provider != HarnessGrok {
		t.Fatalf("omit mint followed seed %q, want config grok", pick.Provider)
	}
	if pick.Knob != KnobConfig {
		t.Fatalf("knob=%q want config", pick.Knob)
	}
	if pick.LosingKnob != KnobCompiledSeed || pick.LosingProvider != HarnessClaude {
		t.Fatalf("loser not named: %+v", pick)
	}
	cite := pick.Cite()
	if !strings.Contains(cite, "provider_knob: config") {
		t.Fatalf("cite missing winning knob: %q", cite)
	}
	if !strings.Contains(cite, "compiled_seed would have picked claude") {
		t.Fatalf("cite missing losing compiled seed: %q", cite)
	}
}

// 🎯T476: leftover llm-portfolio.json (2026-08-09 Claude pin) does not
// silently override config.yaml provider=grok.
func TestPickMintProviderOmitFollowsConfigNotLeftoverFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "llm-portfolio.json")
	if err := os.WriteFile(path, []byte(`{
  "default_provider": "claude",
  "routes": {
    "code_implement": {"prefer": ["claude"], "model": "claude-opus-5"}
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	p, loaded, err := LoadPortfolioOverride(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded {
		t.Fatal("expected override file to load")
	}
	would := p.Route(TaskCodeImplement, nil)
	if would.Provider != HarnessClaude {
		t.Fatalf("fixture: leftover file should pick claude, got %q", would.Provider)
	}
	pick := PickMintProvider(MintProviderArgs{
		ConfigProvider:    HarnessGrok,
		Portfolio:         would,
		PortfolioFromFile: true,
	})
	if pick.Provider != HarnessGrok || pick.Knob != KnobConfig {
		t.Fatalf("leftover file won: %+v", pick)
	}
	if pick.LosingKnob != KnobPortfolioFile || pick.LosingProvider != HarnessClaude {
		t.Fatalf("loser not named: %+v", pick)
	}
	cite := pick.Cite()
	if !strings.Contains(cite, "provider_knob: config") ||
		!strings.Contains(cite, "portfolio_file would have picked claude") {
		t.Fatalf("cite=%q", cite)
	}

	note := DescribeConfigPortfolioDisagreement(HarnessGrok, p, true)
	if note == "" || !strings.Contains(note, "portfolio_file would have picked claude") {
		t.Fatalf("boot disagreement silent: %q", note)
	}
	if DescribeConfigPortfolioDisagreement(HarnessGrok, p, false) != "" {
		t.Fatal("compiled seed is not a leftover file; boot note must stay empty")
	}
}

func TestPickMintProviderExplicitAndResume(t *testing.T) {
	would := RouteDecision{Provider: HarnessClaude, TaskType: TaskCodeImplement, Reason: "fit"}
	explicit := PickMintProvider(MintProviderArgs{
		ProviderArg:    "claude",
		ConfigProvider: HarnessGrok,
		Portfolio:      would,
	})
	if explicit.Provider != HarnessClaude || explicit.Knob != KnobExplicit {
		t.Fatalf("explicit: %+v", explicit)
	}
	if !strings.Contains(explicit.Cite(), "provider_knob: explicit") {
		t.Fatalf("explicit cite=%q", explicit.Cite())
	}

	resume := PickMintProvider(MintProviderArgs{
		Existed:        true,
		StoredProvider: HarnessClaude,
		ConfigProvider: HarnessGrok,
		Portfolio:      would,
	})
	if resume.Provider != HarnessClaude || resume.Knob != KnobResume {
		t.Fatalf("resume: %+v", resume)
	}
	if resume.LosingKnob != "" {
		t.Fatalf("resume must not name a mint loser: %+v", resume)
	}
}

func TestPickMintProviderAgreeingPortfolioNamesConfigOnly(t *testing.T) {
	pick := PickMintProvider(MintProviderArgs{
		ConfigProvider:    HarnessGrok,
		Portfolio:         RouteDecision{Provider: HarnessGrok, TaskType: TaskCodeImplement},
		PortfolioFromFile: true,
	})
	if pick.Provider != HarnessGrok || pick.Knob != KnobConfig || pick.LosingKnob != "" {
		t.Fatalf("agreeing pick: %+v", pick)
	}
	if pick.Cite() != "provider_knob: config" {
		t.Fatalf("cite=%q", pick.Cite())
	}
}

func TestLoadPortfolioOverrideMissingVsPresent(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope.json")
	p, loaded, err := LoadPortfolioOverride(missing)
	if err != nil {
		t.Fatal(err)
	}
	if loaded {
		t.Fatal("missing file must report loaded=false")
	}
	if p.DefaultProvider != HarnessGrok {
		t.Fatalf("missing default=%q", p.DefaultProvider)
	}

	present := filepath.Join(dir, "yes.json")
	if err := os.WriteFile(present, []byte(`{"default_provider":"claude"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	p, loaded, err = LoadPortfolioOverride(present)
	if err != nil || !loaded {
		t.Fatalf("present: loaded=%v err=%v", loaded, err)
	}
	if p.DefaultProvider != "claude" {
		t.Fatalf("loaded default=%q", p.DefaultProvider)
	}
}

func TestPickMintProviderPlanDestWhenDefaultIneligible(t *testing.T) {
	pick := PickMintProvider(MintProviderArgs{
		ConfigProvider:        HarnessGrok,
		PlanFeedOK:            true,
		PlanDest:              HarnessClaude,
		PlanDestOK:            true,
	})
	if pick.Provider != HarnessClaude || pick.Knob != KnobPlanDest {
		t.Fatalf("want plan_dest claude, got %+v", pick)
	}
	if pick.LosingKnob != KnobConfig || pick.LosingProvider != HarnessGrok {
		t.Fatalf("config should lose: %+v", pick)
	}
}

func TestPickMintProviderPlanDestEmptyRefuses(t *testing.T) {
	pick := PickMintProvider(MintProviderArgs{
		ConfigProvider:        HarnessGrok,
		PlanFeedOK:            true,
		PlanDestOK:            false,
	})
	if pick.Provider != "" || pick.Knob != KnobPlanDest {
		t.Fatalf("want empty dest refuse, got %+v", pick)
	}
}

func TestPickMintProviderExplicitStillWinsWhenDefaultIneligible(t *testing.T) {
	pick := PickMintProvider(MintProviderArgs{
		ProviderArg:    HarnessGrok,
		ConfigProvider: HarnessGrok,
		PlanFeedOK:     true,
		PlanDest:       HarnessClaude,
		PlanDestOK:     true,
	})
	if pick.Provider != HarnessGrok || pick.Knob != KnobExplicit {
		t.Fatalf("explicit still wins: %+v", pick)
	}
}

// 🎯T495: usage-first — the plan feed's green pick beats the config
// default even when the default is itself green; config is only a
// tie-break, applied upstream in PickMintDest.
func TestT495PlanPickBeatsGreenConfigDefault(t *testing.T) {
	pick := PickMintProvider(MintProviderArgs{
		ConfigProvider: HarnessGrok,
		PlanFeedOK:     true,
		PlanDest:       HarnessClaude,
		PlanDestOK:     true,
	})
	if pick.Provider != HarnessClaude || pick.Knob != KnobPlanDest {
		t.Fatalf("want plan_dest claude over green config grok, got %+v", pick)
	}
	if pick.LosingKnob != KnobConfig || pick.LosingProvider != HarnessGrok {
		t.Fatalf("config should be the named loser: %+v", pick)
	}
}

// 🎯T495: when the usage-first pick agrees with config (tie-break or
// outright), the start result cites the config knob.
func TestT495PlanPickAgreeingWithConfigCitesConfig(t *testing.T) {
	pick := PickMintProvider(MintProviderArgs{
		ConfigProvider: HarnessGrok,
		PlanFeedOK:     true,
		PlanDest:       HarnessGrok,
		PlanDestOK:     true,
	})
	if pick.Provider != HarnessGrok || pick.Knob != KnobConfig {
		t.Fatalf("want config grok, got %+v", pick)
	}
}

// 🎯T495: with no plan feed at all, mint falls back to config (T476).
func TestT495NoFeedFallsBackToConfig(t *testing.T) {
	pick := PickMintProvider(MintProviderArgs{ConfigProvider: HarnessGrok})
	if pick.Provider != HarnessGrok || pick.Knob != KnobConfig {
		t.Fatalf("no feed must fall back to config: %+v", pick)
	}
}
