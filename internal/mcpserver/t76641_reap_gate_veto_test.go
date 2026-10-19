// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/envelope"
	"github.com/marcelocantos/jevons/internal/gate"
)

// 🎯T766.4.1 clause 3: the reap veto consults typed gate facts — the
// envelope's slots and the gate record they name — and the prose rules of
// gate.FlagFalseGreen stay a report-time banner the reap cannot reach.
// The structural proof is TestT76641ReapDecisionsReachNoProseMatcher
// (scripts/docratchet/t766_4_1_reap_callgraph_test.go); these drive the
// production caller.

// t76641Store points the gate store at a hermetic directory and saves recs.
func t76641Store(t *testing.T, recs ...*gate.Record) {
	t.Helper()
	root := t.TempDir()
	t.Setenv(gate.StoreDirEnv, root)
	store, err := gate.OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range recs {
		if err := store.Save(rec); err != nil {
			t.Fatal(err)
		}
	}
}

func t76641Record(id string, verdict gate.Verdict, exit int) *gate.Record {
	return &gate.Record{
		ID: id, Name: "go-test", Command: []string{"go", "test", "./..."},
		ExitStatus: exit, StatusKnown: true, Verdict: verdict,
		Started: time.Now().Add(-time.Minute), Ended: time.Now(),
	}
}

// declaredGateFinishReport is a valid finish-report whose oracle is a gate
// id slot (and a SHA). The slot is the typed fact the reap veto reads.
func declaredGateFinishReport(target, gateID string, verdict envelope.Verdict, payload string) string {
	return envelope.Format(&envelope.Message{
		Kind:         envelope.KindFinishReport,
		Target:       target,
		SHA:          "abcdef0123456",
		GateID:       gateID,
		Verdict:      verdict,
		SilentLedger: envelope.SilentLedgerEmpty,
		Payload:      payload,
	})
}

// t76641QuotedGreen is a report whose only evidence is a quoted green
// sentence: a GATE line pasted into prose, for an id no store holds, after
// a pipeline that masks the status. Every prose rule of the banner has
// something to say about it.
const t76641QuotedGreen = "Done. I ran `go test ./... | tail -5` and it said:\n\n" +
	"    GATE go-test exit=0 GREEN id=feedface out=000000000000 dur=1.0s\n\n" +
	"All tests pass."

// TestT76641QuotedGreenSentenceIsNeitherReapedNorRegexVetoed drives the
// production reap caller (the agent event sink → maybeReapDoneWorkAgent →
// ShouldAutoReapDoneWorkAgent). A report whose only evidence is a quoted
// green sentence declares nothing, so it is not reaped — and the reason is
// unknown_report, not a false-green veto: the regexes that would have
// vetoed it never ran. The same prose under a declared finish whose typed
// evidence holds up reaps, though the banner still flags the prose.
func TestT76641QuotedGreenSentenceIsNeitherReapedNorRegexVetoed(t *testing.T) {
	t76641Store(t, t76641Record("9f13c0a2", gate.VerdictGreen, 0))
	banner := FalseGreenFlags(t76641QuotedGreen)
	if len(banner) == 0 {
		t.Fatal("control: the report-time banner must flag the quoted sentence, or this tests nothing")
	}

	reg := t439Registry(t, "jv-t76641-quoted")
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-t76641-quoted", t76641QuotedGreen, nil)
	if ok || reason != "unknown_report" {
		t.Fatalf("quoted green sentence: reap=%v reason=%s, want unknown_report", ok, reason)
	}

	cap := &slogCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(cap))
	t.Cleanup(func() { slog.SetDefault(prev) })
	const agent = "jv-t76641-quoted-sink"
	s, sinkReg := reapSinkServer(t, agent)
	s.agentEventSink(agent)(claudia.Event{Type: "assistant", Text: t76641QuotedGreen, StopReason: "end_turn"})
	if sinkReg.Def(agent) == nil {
		t.Fatal("a quoted green sentence reaped the seat")
	}
	for _, r := range cap.snapshot() {
		rec := attrsMap(r)
		if rec["decision"] == "reap_done" && rec["name"] == agent {
			if rec["false_green_flags"] != nil || strings.HasPrefix(stringField(rec, "reason"), "false_green_") {
				t.Fatalf("the reap path logged a prose false-green veto: %v", rec)
			}
		}
	}

	declared := declaredGateFinishReport("T766.4.1", "9f13c0a2", envelope.VerdictGreen, t76641QuotedGreen)
	if len(FalseGreenFlags(declared)) == 0 {
		t.Fatal("control: the banner must still flag the prose under a declared finish")
	}
	if ok, reason := ShouldAutoReapDoneWorkAgent(t439Registry(t, "jv-t76641-declared"), "jv-t76641-declared", declared, nil); !ok {
		t.Fatalf("the reap veto read the payload: reason %s", reason)
	}
	const doneAgent = "jv-t76641-declared-sink"
	s2, reg2 := reapSinkServer(t, doneAgent)
	s2.agentEventSink(doneAgent)(claudia.Event{Type: "assistant", Text: declared, StopReason: "end_turn"})
	if reg2.Def(doneAgent) != nil {
		t.Fatal("a declared finish with a GREEN gate record was vetoed by its prose")
	}
}

func stringField(rec map[string]any, k string) string {
	s, _ := rec[k].(string)
	return s
}

// TestT76641TypedGateFactsVeto pins each typed veto on the reap decision.
func TestT76641TypedGateFactsVeto(t *testing.T) {
	t76641Store(t,
		t76641Record("9f13c0a2", gate.VerdictGreen, 0),
		t76641Record("2817ca36", gate.VerdictRed, 1),
		t76641Record("5b1e0c77", gate.VerdictKilled, 137),
		t76641Record("d1d1d1d1", gate.VerdictDirty, 0),
	)
	for _, tc := range []struct {
		name, report, wantReason string
	}{
		{"green record reaps", declaredGateFinishReport("T1", "9f13c0a2", envelope.VerdictGreen, "Landed."), ""},
		{"red record", declaredGateFinishReport("T1", "2817ca36", envelope.VerdictGreen, "Landed."), "false_green_attestation_not_green"},
		{"killed record", declaredGateFinishReport("T1", "5b1e0c77", envelope.VerdictGreen, "Landed."), "false_green_attestation_killed"},
		{"dirty record", declaredGateFinishReport("T1", "d1d1d1d1", envelope.VerdictGreen, "Landed."), "false_green_attestation_not_green"},
		{"no record", declaredGateFinishReport("T1", "0badc0de", envelope.VerdictGreen, "Landed."), "false_green_attestation_unknown"},
		{"own verdict slot not a pass", declaredGateFinishReport("T1", "", envelope.VerdictRed, "Landed."), "false_green_attestation_not_green"},
		{"declared control is not the oracle",
			envelope.Format(&envelope.Message{
				Kind: envelope.KindFinishReport, Target: "T1", SHA: "abcdef0123456",
				GateID: "2817ca36", GateRoles: []envelope.GateRole{{ID: "2817ca36", Role: envelope.GateRoleControl}},
				SilentLedger: envelope.SilentLedgerEmpty, Payload: "Red-before control.",
			}), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg := t439Registry(t, "jv-t76641-typed")
			ok, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-t76641-typed", tc.report, nil)
			if tc.wantReason == "" {
				if !ok {
					t.Fatalf("reap=false reason=%s, want a reap", reason)
				}
				return
			}
			if ok || reason != tc.wantReason {
				t.Fatalf("reap=%v reason=%s, want %s", ok, reason, tc.wantReason)
			}
		})
	}
}
