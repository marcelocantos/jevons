// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package rsi

import (
	"strings"
	"testing"
)

func TestClassifyFilingQualityRejectsBareFriction(t *testing.T) {
	q := ClassifyFilingQuality("friction: timeout", nil, "")
	if q.OK {
		t.Fatal("bare phrase-friction leaf must be refused")
	}
	joined := strings.Join(q.Reasons, "\n")
	for _, want := range []string{"bare_phrase_friction", "missing_acceptance", "missing_evidence"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("reasons missing %q: %v", want, q.Reasons)
		}
	}
}

func TestClassifyFilingQualityRejectsPlaceholderAcceptance(t *testing.T) {
	q := ClassifyFilingQuality(
		"Fleet spawn errors are diagnosed and eliminated",
		[]string{DefaultFilingAcceptance},
		"session-abc123",
	)
	if q.OK {
		t.Fatal("placeholder-only acceptance must be refused")
	}
	if !strings.Contains(strings.Join(q.Reasons, "\n"), "missing_acceptance") {
		t.Fatalf("want missing_acceptance, got %v", q.Reasons)
	}
}

func TestClassifyFilingQualityAcceptsConcreteFiling(t *testing.T) {
	q := ClassifyFilingQuality(
		"Fleet spawn errors surface in agent_list within one cycle",
		[]string{"Hermetic test: spawn failure fixture → agent_list shows error marker"},
		"eventlog corr=abc123",
	)
	if !q.OK {
		t.Fatalf("concrete filing refused: %v", q.Reasons)
	}
}

func TestIsBarePhraseFrictionName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"friction: timeout", true},
		{"pain: slow builds", true},
		{"timeout", true},
		{"fix it", true},
		{"friction: owner repeats restart requests because daemon stales", false},
		{"Chat scroll keeps jumping to bottom during streaming", false},
	}
	for _, tc := range cases {
		if got := IsBarePhraseFrictionName(tc.name); got != tc.want {
			t.Errorf("IsBarePhraseFrictionName(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}
