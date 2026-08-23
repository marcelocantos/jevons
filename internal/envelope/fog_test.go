// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package envelope

import (
	"strings"
	"testing"
)

func TestScoutReportRoundTripWithFogAndLedger(t *testing.T) {
	m := &Message{
		Kind:         KindScoutReport,
		Target:       "T536.3",
		Phase:        PhaseScout,
		SilentLedger: SilentLedgerRanked,
		Decisions: []SilentDecision{
			{Confidence: 0.45, Choice: "kind=scout-report not finish-report", Why: "T165 must not reap scout as product-done"},
			{Confidence: 0.7, Choice: "reuse T536.1 ledger on spawn-brief", Why: "implementer inherits pre-build decisions"},
		},
		FogKnown:     []string{"T509 envelopes", "T536.1 silent-ledger", "T165 reaps KindFinishReport"},
		FogUnknown:   []string{"whether auto-spawn always scouts"},
		FogBlindspot: []string{"non-trivial threshold for mandatory scout"},
		Payload:      "Fog map + decision table.",
	}
	raw := Format(m)
	for _, want := range []string{
		"jevons: kind scout-report",
		"jevons: phase scout",
		"jevons: silent-ledger ranked",
		"jevons: fog-known",
		"jevons: fog-unknown",
		"jevons: fog-blindspot",
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("missing %q in:\n%s", want, raw)
		}
	}
	got, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v\n%s", err, raw)
	}
	if got.Kind != KindScoutReport || !got.Phase.IsScout() {
		t.Fatalf("kind=%s phase=%s", got.Kind, got.Phase)
	}
	fog := got.Fog()
	if len(fog.Known) != 3 || len(fog.Unknown) != 1 || len(fog.Blindspot) != 1 {
		t.Fatalf("fog=%+v", fog)
	}
	if len(got.SilentDecisions()) != 2 {
		t.Fatalf("decisions=%v", got.SilentDecisions())
	}
}

func TestSpawnBriefDistinguishesScoutVsImplement(t *testing.T) {
	scout := "```jevons\njevons: kind spawn-brief\njevons: target T536.3\njevons: phase scout\n```\n\nSweep fog."
	impl := "```jevons\njevons: kind spawn-brief\njevons: target T536.3\njevons: phase implement\n```\n\nBuild."
	ms, err := Parse(scout)
	if err != nil || ms == nil || !ms.Phase.IsScout() {
		t.Fatalf("scout: %#v err=%v", ms, err)
	}
	mi, err := Parse(impl)
	if err != nil || mi == nil || !mi.Phase.IsImplement() || mi.Phase.IsScout() {
		t.Fatalf("implement: %#v err=%v", mi, err)
	}
	// Absent phase defaults to implement (EffectivePhase).
	legacy := "```jevons\njevons: kind spawn-brief\njevons: target T1\n```\n\nGo."
	ml, err := Parse(legacy)
	if err != nil || ml == nil || EffectivePhase(ml) != PhaseImplement || IsScoutMission(ml) {
		t.Fatalf("legacy: %#v err=%v effective=%s", ml, err, EffectivePhase(ml))
	}
}

func TestInheritLedgerOntoImplementBrief(t *testing.T) {
	scout := &Message{
		Kind:         KindScoutReport,
		Target:       "T536.3",
		SilentLedger: SilentLedgerRanked,
		Decisions: []SilentDecision{
			{Confidence: 0.3, Choice: "scout-report kind", Why: "not product-done"},
		},
		FogKnown: []string{"envelope kinds"},
	}
	brief := InheritLedger(scout, "")
	if brief == nil {
		t.Fatal("expected inherited brief")
	}
	if brief.Kind != KindSpawnBrief || !brief.Phase.IsImplement() {
		t.Fatalf("brief kind/phase: %s %s", brief.Kind, brief.Phase)
	}
	if !brief.HasSilentLedger() || len(brief.SilentDecisions()) != 1 {
		t.Fatalf("ledger not inherited: %+v", brief)
	}
	if len(brief.FogKnown) != 1 {
		t.Fatalf("fog not inherited: %+v", brief.Fog())
	}
	raw := Format(brief)
	got, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse inherited: %v\n%s", err, raw)
	}
	if got.Kind != KindSpawnBrief || got.SilentLedger != SilentLedgerRanked {
		t.Fatalf("got=%+v", got)
	}
}

func TestScoutReportWithoutOracleValidates(t *testing.T) {
	raw := "```jevons\njevons: kind scout-report\njevons: target T536.3\njevons: silent-ledger none\njevons: fog-known T509\n```\n\nNo commits."
	m, err := Parse(raw)
	if err != nil {
		t.Fatalf("scout without oracle must validate: %v", err)
	}
	if m.HasOracle() {
		t.Fatal("unexpected oracle")
	}
}

func TestScoutReportMissingLedgerFlagged(t *testing.T) {
	raw := "```jevons\njevons: kind scout-report\njevons: target T536.3\n```\n\nMap."
	m, err := Parse(raw)
	if m == nil {
		t.Fatal("expected partial message")
	}
	if err == nil {
		t.Fatal("expected validate error for missing ledger")
	}
	if !MissingSilentLedger(m) {
		t.Fatal("MissingSilentLedger should be true")
	}
}

func TestImplementBlockedBySkipRules(t *testing.T) {
	if !MayImplementAfterScout(false, false, false, false) {
		t.Fatal("clear path should allow implement")
	}
	cases := []struct {
		dg, park, fuzzy, sat bool
		want                 string
	}{
		{true, false, false, false, "design-gated"},
		{false, true, false, false, "parked-for-design"},
		{false, false, true, false, "t31.2-fuzzy"},
		{false, false, false, true, "host-saturation-t460"},
	}
	for _, tc := range cases {
		got := ImplementBlockedReason(tc.dg, tc.park, tc.fuzzy, tc.sat)
		if got != tc.want {
			t.Fatalf("got %q want %q", got, tc.want)
		}
		if MayImplementAfterScout(tc.dg, tc.park, tc.fuzzy, tc.sat) {
			t.Fatalf("should block for %q", tc.want)
		}
	}
}

func TestParsePhaseAliases(t *testing.T) {
	if p, ok := ParsePhase("fog-of-war"); !ok || p != PhaseScout {
		t.Fatalf("fog-of-war: %v %v", p, ok)
	}
	if p, ok := ParsePhase("build"); !ok || p != PhaseImplement {
		t.Fatalf("build: %v %v", p, ok)
	}
	if _, ok := ParsePhase("plan"); ok {
		t.Fatal("plan is not a phase — not T254.3 steps")
	}
}

func TestFogBlindspotForcesReslice(t *testing.T) {
	clear := FogMap{Known: []string{"T509"}, Unknown: []string{"auto-spawn?"}}
	if clear.NeedsReslice() {
		t.Fatal("known+unknown without blindspot must not force re-slice")
	}
	hidden := FogMap{Blindspot: []string{"non-trivial threshold hides more map"}}
	if !hidden.NeedsReslice() {
		t.Fatal("blindspot must force re-slice before implement")
	}
	// Scout report with blindspot: inherit ledger still works, but
	// Fog().NeedsReslice signals the PO to carve another scout leaf.
	scout := &Message{
		Kind:         KindScoutReport,
		Target:       "T536.3",
		SilentLedger: SilentLedgerRanked,
		Decisions: []SilentDecision{
			{Confidence: 0.4, Choice: "re-slice before implement", Why: "blindspot remains"},
		},
		FogBlindspot: []string{"mandatory-scout threshold"},
	}
	if !scout.Fog().NeedsReslice() {
		t.Fatal("scout fog with blindspot NeedsReslice")
	}
	brief := InheritLedger(scout, "")
	if brief == nil || !brief.Fog().NeedsReslice() {
		t.Fatal("inherited brief must carry blindspot so implementer sees re-slice signal")
	}
}
