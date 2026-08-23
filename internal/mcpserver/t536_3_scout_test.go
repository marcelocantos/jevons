// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"

	"github.com/marcelocantos/jevons/internal/envelope"
)

// 🎯T536.3 — scout vs implement envelopes; scout without commits is not
// reaped as product-done; implementer brief can inherit the ledger.

func TestT5363ScoutReportIsNotProductDone(t *testing.T) {
	raw := envelope.Format(&envelope.Message{
		Kind:         envelope.KindScoutReport,
		Target:       "T536.3",
		Phase:        envelope.PhaseScout,
		SilentLedger: envelope.SilentLedgerRanked,
		Decisions: []envelope.SilentDecision{
			{Confidence: 0.4, Choice: "scout-report kind", Why: "not finish-report"},
		},
		FogKnown:   []string{"T509 kinds"},
		FogUnknown: []string{"auto-spawn scout default"},
		Payload:    "Scout complete — no implementation commits.",
	})
	if LooksLikeFinishedWorkReport(raw) {
		t.Fatal("scout-report must not look like finished product work")
	}
	if ClassifyCompletionReport(raw) != CompletionNoClaim {
		t.Fatalf("class=%s want no_claim", ClassifyCompletionReport(raw))
	}
}

func TestT5363FinishReportStillReaps(t *testing.T) {
	raw := envelope.Format(&envelope.Message{
		Kind:         envelope.KindFinishReport,
		Target:       "T536.3",
		SHA:          "abcdef0123456",
		SilentLedger: envelope.SilentLedgerEmpty,
		Payload:      "Done.",
	})
	if !LooksLikeFinishedWorkReport(raw) {
		t.Fatal("finish-report still product-done")
	}
}

func TestT5363ImplementBriefCarriesInheritedLedger(t *testing.T) {
	scout := &envelope.Message{
		Kind:         envelope.KindScoutReport,
		Target:       "T536.3",
		SilentLedger: envelope.SilentLedgerRanked,
		Decisions: []envelope.SilentDecision{
			{Confidence: 0.35, Choice: "phase slot on spawn-brief", Why: "distinguish scout vs implement"},
			{Confidence: 0.6, Choice: "fog slots on scout-report", Why: "known/unknown/blindspot machine-checkable"},
		},
		FogBlindspot: []string{"non-trivial threshold"},
	}
	brief := envelope.InheritLedger(scout, "T536.3")
	if brief == nil {
		t.Fatal("nil brief")
	}
	raw := envelope.Format(brief)
	got, err := envelope.Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v\n%s", err, raw)
	}
	if got.Kind != envelope.KindSpawnBrief || !got.Phase.IsImplement() {
		t.Fatalf("kind/phase=%s/%s", got.Kind, got.Phase)
	}
	if !got.HasSilentLedger() || len(got.SilentDecisions()) != 2 {
		t.Fatalf("inherited ledger missing: %+v", got)
	}
}

func TestT5363SpawnBriefPhaseDistinguishes(t *testing.T) {
	scoutBrief := "```jevons\njevons: kind spawn-brief\njevons: target T536.3\njevons: phase scout\n```\n\nFog sweep."
	implBrief := "```jevons\njevons: kind spawn-brief\njevons: target T536.3\njevons: phase implement\njevons: silent-ledger none\n```\n\nBuild."
	ms, err := envelope.Parse(scoutBrief)
	if err != nil || !ms.Phase.IsScout() {
		t.Fatalf("scout brief: %#v %v", ms, err)
	}
	mi, err := envelope.Parse(implBrief)
	if err != nil || !mi.Phase.IsImplement() || mi.Phase.IsScout() {
		t.Fatalf("impl brief: %#v %v", mi, err)
	}
}

func TestT5363SkipRulesBlockImplement(t *testing.T) {
	if envelope.MayImplementAfterScout(true, false, false, false) {
		t.Fatal("design-gated must not punch through to implement")
	}
	if envelope.MayImplementAfterScout(false, false, false, true) {
		t.Fatal("host saturation T460 must block")
	}
	if !envelope.MayImplementAfterScout(false, false, false, false) {
		t.Fatal("clear path allows implement")
	}
}
