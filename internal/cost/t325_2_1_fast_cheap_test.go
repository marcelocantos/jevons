// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"strings"
	"testing"
)

func TestFastCheapClassSeededWithSparkAndGrokPeer(t *testing.T) {
	p := DefaultPortfolio()
	for _, tt := range []string{TaskMechanical, TaskOpsClassify} {
		r, ok := p.Routes[tt]
		if !ok {
			t.Fatalf("missing route %q", tt)
		}
		if r.Class != ClassFastCheap {
			t.Fatalf("%s class=%q want %s", tt, r.Class, ClassFastCheap)
		}
		if r.Models[HarnessCodex] != ModelCodexSpark {
			t.Fatalf("%s codex model=%q want %s", tt, r.Models[HarnessCodex], ModelCodexSpark)
		}
		if r.Models[HarnessGrok] != ModelGrokFast {
			t.Fatalf("%s grok model=%q want %s", tt, r.Models[HarnessGrok], ModelGrokFast)
		}
	}
	// Implementation / overseer-CEO are not in the class.
	for _, tt := range []string{TaskCEO, TaskCodeImplement, TaskDesignProse, TaskIdeation, TaskJourneyGrok} {
		r := p.Routes[tt]
		if r.Class == ClassFastCheap {
			t.Fatalf("%s must not be fast-cheap", tt)
		}
		if r.Models[HarnessCodex] == ModelCodexSpark || r.Models[HarnessGrok] == ModelGrokFast {
			t.Fatalf("%s leaked a fast-cheap model pin: %+v", tt, r.Models)
		}
		d := p.Route(tt, nil)
		if d.Model == ModelCodexSpark || d.Model == ModelGrokFast {
			t.Fatalf("%s Route model=%q is fast-cheap", tt, d.Model)
		}
	}
}

func TestRoutePinsFastCheapOnMechanicalAndOps(t *testing.T) {
	p := DefaultPortfolio()
	mech := p.Route(TaskMechanical, nil)
	if mech.Provider != HarnessGrok || mech.Model != ModelGrokFast {
		t.Fatalf("mechanical → %+v want grok/%s", mech, ModelGrokFast)
	}
	ops := p.Route(TaskOpsClassify, nil)
	if ops.Provider != HarnessGrok || ops.Model != ModelGrokFast {
		t.Fatalf("ops_classify → %+v want grok/%s", ops, ModelGrokFast)
	}
	if NormalizeTaskType("nudge") != TaskMechanical || NormalizeTaskType("ack") != TaskMechanical {
		t.Fatal("nudge/ack aliases")
	}
}

func TestPickMintModelFastCheapBothPeers(t *testing.T) {
	p := DefaultPortfolio()
	grok := PickMintModel(MintModelArgs{
		Provider: HarnessGrok, TaskType: TaskMechanical,
		CodexEligible: true, Portfolio: p,
	})
	if grok.Model != ModelGrokFast || grok.Knob != KnobFastCheap {
		t.Fatalf("grok mechanical: %+v", grok)
	}
	codex := PickMintModel(MintModelArgs{
		Provider: HarnessCodex, TaskType: TaskOpsClassify,
		CodexEligible: true, Portfolio: p,
	})
	if codex.Model != ModelCodexSpark || codex.Knob != KnobFastCheap {
		t.Fatalf("codex ops: %+v", codex)
	}
	if !strings.Contains(codex.Cite(), "model_knob: fast_cheap") ||
		!strings.Contains(codex.Cite(), ModelCodexSpark) {
		t.Fatalf("cite=%q", codex.Cite())
	}
}

func TestPickMintModelDoesNotPinImplementationOrOverseer(t *testing.T) {
	p := DefaultPortfolio()
	for _, tt := range []string{TaskCodeImplement, TaskCEO} {
		for _, prov := range []string{HarnessGrok, HarnessCodex} {
			got := PickMintModel(MintModelArgs{
				Provider: prov, TaskType: tt,
				CodexEligible: true, Portfolio: p,
			})
			if got.Model == ModelCodexSpark || got.Model == ModelGrokFast {
				t.Fatalf("%s/%s pinned fast-cheap: %+v", tt, prov, got)
			}
			if got.Knob == KnobFastCheap {
				t.Fatalf("%s/%s knob fast_cheap", tt, prov)
			}
		}
	}
}

func TestPickMintModelExplicitAndResumeWin(t *testing.T) {
	p := DefaultPortfolio()
	exp := PickMintModel(MintModelArgs{
		ModelArg: "grok-4.5", Provider: HarnessGrok, TaskType: TaskMechanical,
		CodexEligible: true, Portfolio: p,
	})
	if exp.Model != "grok-4.5" || exp.Knob != KnobExplicit {
		t.Fatalf("explicit: %+v", exp)
	}
	res := PickMintModel(MintModelArgs{
		Existed: true, StoredModel: "grok-4.5",
		Provider: HarnessGrok, TaskType: TaskMechanical,
		CodexEligible: true, Portfolio: p,
	})
	if res.Model != "grok-4.5" || res.Knob != KnobModelResume {
		t.Fatalf("resume: %+v", res)
	}
	if res.Cite() != "" {
		t.Fatalf("resume must not cite a mint pin: %q", res.Cite())
	}
}

func TestPickMintModelSkipsSparkOnRedCodexWeekly(t *testing.T) {
	p := DefaultPortfolio()
	got := PickMintModel(MintModelArgs{
		Provider: HarnessCodex, TaskType: TaskMechanical,
		CodexEligible: false, Portfolio: p,
	})
	if got.Model == ModelCodexSpark {
		t.Fatalf("pinned Spark onto red Codex weekly: %+v", got)
	}
	if got.Knob != KnobIneligible {
		t.Fatalf("knob=%q want ineligible", got.Knob)
	}
	if !strings.Contains(got.Cite(), "T390.1.5") {
		t.Fatalf("ineligibility must be visible: %q", got.Cite())
	}
	// Grok dest is unaffected.
	g := PickMintModel(MintModelArgs{
		Provider: HarnessGrok, TaskType: TaskMechanical,
		CodexEligible: false, Portfolio: p,
	})
	if g.Model != ModelGrokFast {
		t.Fatalf("grok dest should still pin fast peer: %+v", g)
	}
}

func TestPickMintModelEscalatesWhenPromptExceedsWindow(t *testing.T) {
	p := DefaultPortfolio()
	spark := PickMintModel(MintModelArgs{
		Provider: HarnessCodex, TaskType: TaskMechanical,
		PromptTokens: ContextCodexSpark + 1, CodexEligible: true, Portfolio: p,
	})
	if spark.Model != "" || spark.Knob != KnobEscalated {
		t.Fatalf("spark over window: %+v", spark)
	}
	if !strings.Contains(spark.Reason, ModelCodexSpark) || !strings.Contains(spark.Cite(), "escalated") {
		t.Fatalf("escalate must name the model: %+v cite=%q", spark, spark.Cite())
	}
	grok := PickMintModel(MintModelArgs{
		Provider: HarnessGrok, TaskType: TaskOpsClassify,
		PromptTokens: ContextGrokFast + 1, CodexEligible: true, Portfolio: p,
	})
	if grok.Model != "" || grok.Knob != KnobEscalated {
		t.Fatalf("grok over window: %+v", grok)
	}
	if !strings.Contains(grok.Reason, ModelGrokFast) {
		t.Fatalf("grok escalate reason=%q", grok.Reason)
	}
	// Under the window still pins.
	under := PickMintModel(MintModelArgs{
		Provider: HarnessCodex, TaskType: TaskMechanical,
		PromptTokens: ContextCodexSpark, CodexEligible: true, Portfolio: p,
	})
	if under.Model != ModelCodexSpark {
		t.Fatalf("at-window should still pin: %+v", under)
	}
}

func TestClaudeHasNoFastCheapPeer(t *testing.T) {
	got := PickMintModel(MintModelArgs{
		Provider: HarnessClaude, TaskType: TaskMechanical, CodexEligible: true,
	})
	if got.Model != "" || got.Knob != "" {
		t.Fatalf("claude must not invent a Spark peer: %+v", got)
	}
}

func TestModelContextWindows(t *testing.T) {
	if ModelContextWindow(ModelCodexSpark) != ContextCodexSpark {
		t.Fatal("spark window")
	}
	if ModelContextWindow(ModelGrokFast) != ContextGrokFast {
		t.Fatal("grok-build window")
	}
	if ModelContextWindow("grok-4.5") != 0 {
		t.Fatal("unknown is not a hard block")
	}
}

func TestEstimatePromptTokens(t *testing.T) {
	if EstimatePromptTokens("") != 0 {
		t.Fatal("empty")
	}
	if n := EstimatePromptTokens("abcd"); n != 1 {
		t.Fatalf("4 runes → %d want 1", n)
	}
}
