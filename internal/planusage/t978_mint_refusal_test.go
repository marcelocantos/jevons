// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"strings"
	"testing"
	"time"
)

// 🎯T978: a refused mint names each plan and why it cannot take a seat. The
// 2026-10-01 state: Claude weekly ahead of pace, Grok and Cursor held off by
// the owner, Codex exhausted; the refusal said only "no catalog model
// matches predicates".
func TestT978MintRefusalNamesEachPlansReason(t *testing.T) {
	now := time.Date(2026, 10, 1, 4, 0, 0, 0, time.UTC)
	th := DefaultThresholds()
	plan := func(p string, used float64, frac float64) Backend {
		return Backend{Provider: p, Status: StatusAvailable,
			Windows: []Window{bandWindow(WindowWeekly, used, 100-used, now, frac)}}
	}
	claude := plan("claude", 62, 0.6) // 62% used with 60% of the week left
	grok := plan("grok", 91, 0.5)
	grok.Override = &Override{Band: BandExhausted, Reason: "quota dangerously low"}
	codex := plan("codex", 100, 0.5)
	if DestEligible(claude, now, th) {
		t.Fatalf("fixture: claude should not be a dest (band %s)", WeeklyBandOf(claude, now, th))
	}
	got := MintRefusalDetail([]DestCand{
		{Provider: "claude", Backend: claude},
		{Provider: "grok", Backend: grok},
		{Provider: "codex", Backend: codex},
		{Provider: "bedrock", Backend: Backend{Provider: "bedrock", Status: "unavailable"}},
	}, now, th)
	for _, want := range []string{
		"claude: weekly ahead of pace (62% used)",
		"grok: owner override exhausted (quota dangerously low)",
		"codex: weekly exhausted",
		"bedrock: no reading (unavailable)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("refusal %q lacks %q", got, want)
		}
	}
}
