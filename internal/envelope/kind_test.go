// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package envelope

import "testing"

func TestAllKindsAreParseableAndUnique(t *testing.T) {
	seen := map[Kind]bool{}
	for _, k := range AllKinds() {
		if k == "" {
			t.Fatal("empty kind")
		}
		if seen[k] {
			t.Fatalf("duplicate kind %s", k)
		}
		seen[k] = true
		got, ok := ParseKind(string(k))
		if !ok || got != k {
			t.Fatalf("ParseKind(%q)=%q ok=%v", k, got, ok)
		}
	}
	want := []Kind{
		KindSpawnBrief, KindFinishReport, KindStatusPing,
		KindEscalation, KindAck, KindTargetFileRequest,
	}
	if len(AllKinds()) != len(want) {
		t.Fatalf("AllKinds len=%d want %d — update instruction ratchets when kinds change", len(AllKinds()), len(want))
	}
}

func TestVocabularyParse(t *testing.T) {
	if v, ok := ParseVerdict("green"); !ok || v != VerdictGreen {
		t.Fatalf("GREEN: %v %v", v, ok)
	}
	if v, ok := ParseVerdict("SUSPECT"); !ok || v != VerdictSuspect {
		t.Fatalf("SUSPECT: %v %v", v, ok)
	}
	if _, ok := ParseVerdict("pass"); ok {
		t.Fatal("pass is not a verdict — GREEN is the only pass word")
	}
	if p, ok := ParseProgress("in progress"); !ok || p != ProgressInProgress {
		t.Fatalf("in-progress: %v %v", p, ok)
	}
	if p, ok := ParseProgress("live"); !ok || !p.ProductVisible() {
		t.Fatalf("live: %v %v", p, ok)
	}
	if r, ok := ParseRisk("class-3"); !ok || !r.IsAccepted() {
		t.Fatalf("class-3: %v %v", r, ok)
	}
	if r, ok := ParseRisk("residual"); !ok || !r.IsAccepted() {
		t.Fatalf("residual: %v %v", r, ok)
	}
	if r, ok := ParseRisk("none"); !ok || r.IsAccepted() {
		t.Fatalf("none: %v %v", r, ok)
	}
}

func TestLoadBearingAndChatterSets(t *testing.T) {
	if !KindFinishReport.LoadBearing() || KindAck.LoadBearing() {
		t.Fatal("finish-report is load-bearing; ack is not")
	}
	if !KindStatusPing.ChatterCapped() || KindFinishReport.ChatterCapped() {
		t.Fatal("status-ping is chatter-capped; finish-report is not")
	}
}
