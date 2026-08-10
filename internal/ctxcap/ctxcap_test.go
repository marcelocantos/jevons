// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package ctxcap

import (
	"testing"
	"time"
)

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

// The treadmill this exists to stop. Observed 2026-08-10 with no
// hysteresis: the overseer rotated five times in 23 minutes (13:08,
// 13:12, 13:16, 13:23, 13:31) and then stayed down. Compaction hands the
// successor its predecessor's transcript, which it reads, which puts it
// straight back over the ceiling.
func TestRecentCompactionHoldsRatherThanThrashing(t *testing.T) {
	p := Policy{Ceiling: 100_000, MinInterval: 30 * time.Minute}
	over := Observation{Agent: "jevons", Context: 250_000, HasContext: true}

	// Never compacted: act.
	if got := p.Evaluate(over).Verdict; got != VerdictCompact {
		t.Errorf("first compaction verdict=%s want compact", got)
	}
	// Compacted four minutes ago and already back over: hold, do not
	// rotate again — that is the treadmill.
	recent := over
	recent.SinceLastCompaction = 4 * time.Minute
	d := p.Evaluate(recent)
	if d.Verdict != VerdictHold {
		t.Fatalf("verdict=%s want hold", d.Verdict)
	}
	if Compactions([]Decision{d}) != 0 {
		t.Error("a hold must not count as a compaction")
	}
	// A hold is NOT ok: an agent living above the ceiling must stay
	// visible rather than passing as healthy.
	if d.Verdict == VerdictOK {
		t.Error("hold collapsed into ok — a persistent hold is a configuration signal")
	}
	// Past the interval: act again.
	old := over
	old.SinceLastCompaction = 31 * time.Minute
	if got := p.Evaluate(old).Verdict; got != VerdictCompact {
		t.Errorf("after the interval verdict=%s want compact", got)
	}
}

func TestMinIntervalDefaultsAndDisable(t *testing.T) {
	if got := (Policy{}).EffectiveMinInterval(); got != DefaultMinInterval {
		t.Errorf("unset=%s want %s", got, DefaultMinInterval)
	}
	// Negative disables hysteresis — tests only; it is what produced the
	// observed treadmill.
	p := Policy{Ceiling: 100_000, MinInterval: -1}
	obs := Observation{Agent: "a", Context: 200_000, HasContext: true, SinceLastCompaction: time.Second}
	if got := p.Evaluate(obs).Verdict; got != VerdictCompact {
		t.Errorf("hysteresis disabled verdict=%s want compact", got)
	}
}
