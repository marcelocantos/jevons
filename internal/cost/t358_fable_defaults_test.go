// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import "testing"

func TestNormalizeFableRatification(t *testing.T) {
	if NormalizeFableRatification("") != FableRatifiedKeepCurrent {
		t.Fatal("empty")
	}
	if NormalizeFableRatification("both") != FableRatifiedBoth {
		t.Fatal("both")
	}
}

func TestFableClassMintModelPOOnlyWhenRatified(t *testing.T) {
	base := MintModelArgs{
		Provider: HarnessClaude,
		TaskType: TaskCEO,
		Role:     "product-owner",
	}
	if got := FableClassMintModel(base); got.Model != "" {
		t.Fatalf("unratified should not pin: %+v", got)
	}
	base.FableRatified = FableRatifiedPOFable
	got := FableClassMintModel(base)
	if got.Model != ModelClaudeFableClass || got.Knob != KnobFableClassDefault {
		t.Fatalf("po_fable: %+v", got)
	}
}

func TestFableClassMintModelTieredSkipsPO(t *testing.T) {
	got := FableClassMintModel(MintModelArgs{
		Provider:      HarnessClaude,
		TaskType:      TaskCEO,
		Role:          "product-owner",
		FableRatified: FableRatifiedTiered,
	})
	if got.Model != "" {
		t.Fatalf("tiered must not pin PO: %+v", got)
	}
}

func TestPickMintModelFableRatifiedWinsOverFastCheap(t *testing.T) {
	// CEO is not fast-cheap; PO with po_fable on Claude pins Fable.
	got := PickMintModel(MintModelArgs{
		Provider:      HarnessClaude,
		TaskType:      TaskCEO,
		Role:          "product-owner",
		FableRatified: FableRatifiedPOFable,
	})
	if got.Model != ModelClaudeFableClass {
		t.Fatalf("got %+v", got)
	}
}

func TestEffectiveOverseerModelRatifiedRoot(t *testing.T) {
	if m := EffectiveOverseerModel("", "", FableRatifiedRootFable, HarnessClaude); m != ModelClaudeFableClass {
		t.Fatalf("want fable, got %q", m)
	}
	if m := EffectiveOverseerModel("claude-opus-5", "", FableRatifiedBoth, HarnessClaude); m != "claude-opus-5" {
		t.Fatalf("explicit overseer_model wins: %q", m)
	}
}
