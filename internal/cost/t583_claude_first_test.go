// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"strings"
	"testing"
)

func t583pf(v float64) *float64 { return &v }

// 🎯T583 tape 1: omitted provider with Claude headroom lands on claude for
// every task class — config grok and the portfolio's own pick both lose,
// and the citation carries the headroom figure.
func TestT583OmitProviderLandsOnClaudeWithHeadroom(t *testing.T) {
	for _, tt := range []string{TaskCodeImplement, TaskCEO, TaskMechanical, TaskOpsClassify, TaskDesignProse, TaskIdeation} {
		pick := PickMintProvider(MintProviderArgs{
			ConfigProvider: HarnessGrok,
			Portfolio:      RouteDecision{Provider: HarnessGrok, TaskType: tt},
			PlanFeedOK:     true,
			PlanDest:       HarnessGrok,
			PlanDestOK:     true,
			ClaudeFirstOK:  true,
			ClaudeHeadroom: t583pf(56),
		})
		if pick.Provider != HarnessClaude || pick.Knob != KnobClaudeFirst {
			t.Fatalf("%s: pick=%+v want claude/claude-first", tt, pick)
		}
		cite := pick.Cite()
		if !strings.Contains(cite, "provider_knob: claude-first: plan headroom 56%") {
			t.Fatalf("%s: cite=%q", tt, cite)
		}
		if pick.LosingKnob != KnobConfig || pick.LosingProvider != HarnessGrok {
			t.Fatalf("%s: loser=%s/%s want config/grok", tt, pick.LosingKnob, pick.LosingProvider)
		}
	}
}

// 🎯T583 tape 2: Claude exhausted (0% / 429) → the mint falls back to the
// usage-first dest and the citation names the knob that actually decided.
func TestT583ExhaustedClaudeFallsBackWithCitation(t *testing.T) {
	pick := PickMintProvider(MintProviderArgs{
		ConfigProvider: HarnessGrok,
		Portfolio:      RouteDecision{Provider: HarnessClaude, TaskType: TaskCodeImplement},
		PlanFeedOK:     true,
		PlanDest:       HarnessCodex,
		PlanDestOK:     true,
		ClaudeFirstOK:  false,
	})
	if pick.Provider != HarnessCodex || pick.Knob != KnobPlanDest {
		t.Fatalf("pick=%+v want codex/plan_dest", pick)
	}
	if cite := pick.Cite(); !strings.Contains(cite, "provider_knob: plan_dest") {
		t.Fatalf("cite=%q", cite)
	}
	// No green anywhere still refuses rather than landing on a hot dest.
	refuse := PickMintProvider(MintProviderArgs{
		ConfigProvider: HarnessGrok,
		PlanFeedOK:     true,
		PlanDestOK:     false,
	})
	if refuse.Provider != "" || refuse.Knob != KnobPlanDest {
		t.Fatalf("refuse=%+v", refuse)
	}
}

// 🎯T583 tape 3: an explicit provider= still wins over claude-first, and so
// does a stored provider on resume — claude-first is a mint knob only.
func TestT583ExplicitAndResumeBeatClaudeFirst(t *testing.T) {
	explicit := PickMintProvider(MintProviderArgs{
		ProviderArg:    HarnessGrok,
		ConfigProvider: HarnessGrok,
		ClaudeFirstOK:  true,
		ClaudeHeadroom: t583pf(56),
	})
	if explicit.Provider != HarnessGrok || explicit.Knob != KnobExplicit {
		t.Fatalf("explicit=%+v", explicit)
	}
	resume := PickMintProvider(MintProviderArgs{
		Existed:        true,
		StoredProvider: HarnessCodex,
		ConfigProvider: HarnessGrok,
		ClaudeFirstOK:  true,
		ClaudeHeadroom: t583pf(56),
	})
	if resume.Provider != HarnessCodex || resume.Knob != KnobResume {
		t.Fatalf("resume=%+v", resume)
	}
}

// 🎯T583: owner_asked is the third exit — the owner may send the fleet
// elsewhere without naming a provider on every single mint.
func TestT583OwnerAskedSkipsClaudeFirst(t *testing.T) {
	pick := PickMintProvider(MintProviderArgs{
		ConfigProvider: HarnessGrok,
		OwnerAsked:     true,
		PlanFeedOK:     true,
		PlanDest:       HarnessGrok,
		PlanDestOK:     true,
		ClaudeFirstOK:  true,
		ClaudeHeadroom: t583pf(56),
	})
	if pick.Provider != HarnessGrok || pick.Knob == KnobClaudeFirst {
		t.Fatalf("owner_asked=%+v", pick)
	}
}

// 🎯T583: an unquantified Claude is cited as unknown, never as 0%.
func TestT583UnknownHeadroomNote(t *testing.T) {
	if got := ClaudeHeadroomNote(nil); got != "plan headroom unknown" {
		t.Fatalf("nil headroom note = %q", got)
	}
	if got := ClaudeHeadroomNote(t583pf(42.4)); got != "plan headroom 42%" {
		t.Fatalf("note = %q", got)
	}
}

// 🎯T583: the compiled task-class seed no longer routes a default work mint
// to Grok — the incident's "would have picked grok via code_implement" line
// cannot come back from the seed.
func TestT583CompiledSeedWorkMintIsNotGrok(t *testing.T) {
	if got := WorkMintPortfolioProvider(DefaultPortfolio()); got == HarnessGrok {
		t.Fatalf("compiled seed work mint = grok; want claude-first-compatible route")
	} else if got != HarnessClaude {
		t.Fatalf("compiled seed work mint = %q, want claude", got)
	}
}
