// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/envelope"
	"github.com/marcelocantos/jevons/internal/relayroute"
)

// 🎯T577: a worker checkpoint is never reaped as a finished-work report.
//
// The fixture is jv-t568-intent-closed's stored report
// 20260829T075858Z-67c47931 — a depth-ceiling checkpoint jammed after a
// period with no newline, listing remaining work ("Next step: … implement
// … achieve, finish-report"). The reap path read "achieved" plus "commit"
// / "make test-go" as finished_work and deregistered the seat.

func loadT577Fixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "t577_jv_t568_checkpoint_report.md"))
	if err != nil {
		t.Fatalf("read T577 fixture: %v", err)
	}
	if len(b) < 800 {
		t.Fatalf("fixture too short to be the incident report: %d bytes", len(b))
	}
	return string(b)
}

func TestT577IncidentReportIsTheIncidentShape(t *testing.T) {
	report := loadT577Fixture(t)
	lower := asciiLower(report)
	if !strings.Contains(report, "Checkpoint (turn-depth ceiling)") {
		t.Fatal("fixture lost the jammed checkpoint declaration")
	}
	if !strings.Contains(lower, "next step") {
		t.Fatal("fixture lost its remaining-work next step")
	}
	if !hasCompletionClaim(lower) {
		t.Fatal("fixture no longer carries a completion word — cannot reproduce the incident")
	}
	if !hasOracleEvidence(lower) {
		t.Fatal("fixture no longer carries oracle-shaped words — cannot reproduce the incident")
	}
}

func TestT577IncidentReportIsNotTerminal(t *testing.T) {
	report := loadT577Fixture(t)
	if LooksLikeFinishedWorkReport(report) {
		t.Fatal("jv-t568 checkpoint classified as finished_work")
	}
	if ask := ClassifyReportAsk(report); ask != AskCheckpoint {
		t.Fatalf("ClassifyReportAsk = %s, want checkpoint", ask)
	}
	reg := t395Registry(t, "jv-t568-intent-closed")
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-t568-intent-closed", report, nil)
	if ok {
		t.Fatalf("incident report reaps (reason %s)", reason)
	}
	if !strings.Contains(reason, "checkpoint") && reason != "not_finished_work_report" {
		t.Fatalf("reason = %q, want checkpoint keep", reason)
	}
}

func TestT577IncidentSeatRetainedThroughSink(t *testing.T) {
	report := loadT577Fixture(t)
	const agent = "jv-t568-intent-closed"
	s, reg := t471SinkServer(t, agent)
	sink := s.agentEventSink(agent)
	sink(claudia.Event{
		Type:       "assistant",
		Text:       report,
		StopReason: "end_turn",
	})
	if reg.Def(agent) == nil {
		t.Fatal("checkpoint report deregistered the seat")
	}
}

func TestT577FinishReportEnvelopeStillReaps(t *testing.T) {
	raw := envelope.Format(&envelope.Message{
		Kind:         envelope.KindFinishReport,
		Target:       "T577",
		SHA:          "abcdef0123456",
		SilentLedger: envelope.SilentLedgerEmpty,
		Payload:      "Done. Next step: overseer reviews.",
	})
	if !LooksLikeFinishedWorkReport(raw) {
		t.Fatal("typed finish-report must still reap even with next-step payload")
	}
	reg := t395Registry(t, "jv-t577-envelope")
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-t577-envelope", raw, nil)
	if !ok {
		t.Fatalf("finish-report envelope did not reap (reason %s)", reason)
	}
}

func TestT577ForwardLookingPlanShapesDoNotReap(t *testing.T) {
	cases := []struct {
		name   string
		report string
		want   ReportAskClass
	}{
		{
			name:   "jammed checkpoint after period",
			report: "Surveyed the loader.Checkpoint (turn-depth ceiling). Next step: implement the close rule.",
			want:   AskCheckpoint,
		},
		{
			name:   "next step remaining work",
			report: "Mapped the resume path. TargetID that is achieved in the ledger is the close rule. Next step: implement both rules.",
			want:   AskExplicitIncomplete,
		},
		{
			name:   "i'll resume",
			report: "Diagnosis complete. I'll resume on the writer after this turn.",
			want:   AskExplicitIncomplete,
		},
		{
			name:   "ending the turn here",
			report: "Wired the decoder. Ending the turn here; encoder is next.",
			want:   AskCheckpoint,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyReportAsk(tc.report); got != tc.want {
				t.Fatalf("ClassifyReportAsk = %s, want %s", got, tc.want)
			}
			if LooksLikeFinishedWorkReport(tc.report) {
				t.Fatal("forward-looking plan classified as finished work")
			}
			reg := t395Registry(t, "jv-t577-plan")
			ok, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-t577-plan", tc.report, nil)
			if ok {
				t.Fatalf("reaped (%s)", reason)
			}
		})
	}
}

func TestT577MentionOfCheckpointStillFinishes(t *testing.T) {
	report := "Done. Commit 4f2b8c1; go test PASS. Checkpoint reports now classify as open work."
	if ClassifyReportAsk(report) != AskNone {
		t.Fatalf("mention classified as ask: %s", ClassifyReportAsk(report))
	}
	if !LooksLikeFinishedWorkReport(report) {
		t.Fatal("genuine finish about checkpoint handling must still reap")
	}
}

func TestT577BareDoneStillReaps(t *testing.T) {
	reg := t395Registry(t, "jv-t577-bare")
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-t577-bare", "Done. Mission complete.", nil)
	if !ok {
		t.Fatalf("T195 bare done must still reap; reason %s", reason)
	}
}

func TestT577NoticeStaysOnThePO(t *testing.T) {
	msg := FormatCheckpointReapRespawnNotice("T568", "jv-t568-intent-closed")
	if relayroute.Classify(msg) != relayroute.RouteParent {
		t.Fatalf("respawn notice reroutes off the PO: %s", relayroute.Classify(msg))
	}
	for _, want := range []string{"🎯T577", "🎯T568", "jv-t568-intent-closed", "Respawn"} {
		if !strings.Contains(msg, want) {
			t.Errorf("notice missing %q:\n%s", want, msg)
		}
	}
}

func TestT577MisEnvelopedCheckpointNotifiesPO(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{
		{Name: "jevons-po", WorkDir: dir, SessionID: "po", Purpose: claudia.PurposeWork, Parent: "jevons", Materialized: true, Provider: "grok"},
		{Name: "jv-t577-envelope", WorkDir: dir, SessionID: "w", Purpose: claudia.PurposeWork, Parent: "jevons-po", TargetID: "T577", Materialized: true, Provider: "grok"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	po := &fakeSender{alive: true}
	s.SetSenderResolver(func(name string) (agentSender, bool, error) {
		if name != "jevons-po" {
			t.Fatalf("unexpected fleet delivery to %q", name)
		}
		return po, false, nil
	})
	s.SetTurnWitness(witnessYielding(TurnEvidence{
		Observed: true, PayloadSeen: true,
		Detail: "transcript gained a user message carrying this payload",
	}))

	raw := envelope.Format(&envelope.Message{
		Kind:         envelope.KindFinishReport,
		Target:       "T577",
		SHA:          "abcdef0123456",
		SilentLedger: envelope.SilentLedgerEmpty,
		Payload:      "Checkpoint (turn-depth ceiling). Next step: implement the close rule.",
	})
	s.maybeReapDoneWorkAgent("jv-t577-envelope", raw)
	if reg.Def("jv-t577-envelope") != nil {
		t.Fatal("finish-report envelope must still reap")
	}
	if len(po.sent) != 1 {
		t.Fatalf("PO deliveries = %d, want 1: %v", len(po.sent), po.sent)
	}
	got := po.sent[0]
	for _, want := range []string{"🎯T577", "jv-t577-envelope", "Respawn"} {
		if !strings.Contains(got, want) {
			t.Errorf("PO notice missing %q:\n%s", want, got)
		}
	}
}

func TestT577GenuineFinishDoesNotNotifyPO(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t577-done", WorkDir: dir, SessionID: "w",
		Purpose: claudia.PurposeWork, Parent: "jevons-po", TargetID: "T577",
		Materialized: true, Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	po := &fakeSender{alive: true}
	s.SetSenderResolver(func(name string) (agentSender, bool, error) {
		t.Fatalf("genuine finish must not wake the PO, got %q", name)
		return po, false, nil
	})
	s.maybeReapDoneWorkAgent("jv-t577-done",
		"Done. SHA abcdef0123456. go test ./internal/mcpserver -run T577 PASS")
	if reg.Def("jv-t577-done") != nil {
		t.Fatal("genuine finish must reap")
	}
}
