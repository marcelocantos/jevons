// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package ctxcap

import "testing"

func TestEvaluateCompactsOnlyAboveTheCeiling(t *testing.T) {
	p := Policy{Ceiling: 100_000}
	for _, tc := range []struct {
		name string
		ctx  int64
		want Verdict
	}{
		{"well under", 40_000, VerdictOK},
		{"just under", 99_999, VerdictOK},
		{"exactly at the ceiling", 100_000, VerdictOK},
		{"one over", 100_001, VerdictCompact},
		{"far over", 399_000, VerdictCompact},
	} {
		got := p.Evaluate(Observation{Agent: "a", Context: tc.ctx, HasContext: true})
		if got.Verdict != tc.want {
			t.Errorf("%s: ctx=%d verdict=%s want %s", tc.name, tc.ctx, got.Verdict, tc.want)
		}
		if got.Reason == "" {
			t.Errorf("%s: decision carries no reason", tc.name)
		}
	}
}

// A missing measurement must never be read as a small context. Compacting
// on an unknown would rotate agents at random, which is worse than the
// unbounded growth the ceiling exists to stop.
func TestUnknownContextNeverCompacts(t *testing.T) {
	p := Policy{Ceiling: 100_000}
	d := p.Evaluate(Observation{Agent: "cold", HasContext: false})
	if d.Verdict != VerdictUnknown {
		t.Fatalf("verdict=%s want %s", d.Verdict, VerdictUnknown)
	}
	if Compactions([]Decision{d}) != 0 {
		t.Fatal("an unknown context must not count as a compaction")
	}
}

func TestExemptAgentIsNeverCompacted(t *testing.T) {
	p := Policy{Ceiling: 50_000}
	d := p.Evaluate(Observation{Agent: "jevons", Context: 900_000, HasContext: true, Exempt: true})
	if d.Verdict != VerdictOK {
		t.Fatalf("exempt agent verdict=%s want ok", d.Verdict)
	}
}

func TestDisabledObservesButNeverActs(t *testing.T) {
	p := Policy{Ceiling: 10_000, Disabled: true}
	d := p.Evaluate(Observation{Agent: "a", Context: 5_000_000, HasContext: true})
	if d.Verdict != VerdictOK {
		t.Fatalf("disabled verdict=%s want ok", d.Verdict)
	}
	// The observation still travels, so the spend report can show what the
	// ceiling would have done before anyone turns it on. The ceiling it
	// reports is the effective one — 10k floors to MinCeiling — so a
	// disabled policy and an enabled one describe the same threshold.
	if d.Context != 5_000_000 || d.Ceiling != MinCeiling {
		t.Fatalf("disabled dropped the observation: %+v", d)
	}
}

// A ceiling low enough to cause constant rotation costs more in handovers
// than it saves, so it is raised rather than honoured.
func TestCeilingFloorsAtMinimum(t *testing.T) {
	if got := (Policy{Ceiling: 500}).EffectiveCeiling(); got != MinCeiling {
		t.Errorf("tiny ceiling=%d want %d", got, MinCeiling)
	}
	if got := (Policy{}).EffectiveCeiling(); got != DefaultCeiling {
		t.Errorf("unset ceiling=%d want %d", got, DefaultCeiling)
	}
	if got := (Policy{Ceiling: 250_000}).EffectiveCeiling(); got != 250_000 {
		t.Errorf("explicit ceiling=%d want 250000", got)
	}
}

// The baseline's own coordinator contexts, replayed through the policy:
// every one of them is over a 100k ceiling, which is the finding that
// motivated the target.
func TestBaselineCoordinatorContextsAllExceedDefault(t *testing.T) {
	p := Policy{}
	observed := []Observation{
		{Agent: "jevons-po", Context: 235_000, HasContext: true},
		{Agent: "jevons", Context: 192_000, HasContext: true},
		{Agent: "orthograph-po", Context: 168_000, HasContext: true},
		{Agent: "claudia-po", Context: 135_000, HasContext: true},
	}
	ds := p.EvaluateAll(observed)
	if got := Compactions(ds); got != len(observed) {
		t.Fatalf("compactions=%d want %d — every baseline coordinator ran over 100k", got, len(observed))
	}
}
