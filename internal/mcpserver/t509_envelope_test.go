// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/envelope"
)

func t509Finish(payload string) string {
	return envelope.Format(&envelope.Message{
		Kind:    envelope.KindFinishReport,
		Target:  "T509",
		SHA:     "abcdef0123456",
		GateID:  "9f13c0a2",
		Verdict: envelope.VerdictGreen,
		Status:  envelope.ProgressInProgress,
		Payload: payload,
	})
}

func TestT509ValidFinishReportIsOracleEvidence(t *testing.T) {
	raw := t509Finish("Work landed.")
	if ClassifyCompletionReport(raw) != CompletionOracleEvidence {
		t.Fatalf("class=%s", ClassifyCompletionReport(raw))
	}
	if !HasOracleEvidence(raw) {
		t.Fatal("envelope sha/gate-id must count as oracle")
	}
	if !LooksLikeFinishedWorkReport(raw) {
		t.Fatal("finish-report kind is a finish shape")
	}
}

func TestT509FinishReportRiskField(t *testing.T) {
	raw := envelope.Format(&envelope.Message{
		Kind:    envelope.KindFinishReport,
		Target:  "T509",
		Risk:    envelope.RiskClass3,
		Payload: "Done.",
	})
	if ClassifyCompletionReport(raw) != CompletionAcceptedRisk {
		t.Fatalf("class=%s", ClassifyCompletionReport(raw))
	}
	if !HasAcceptedRisk(raw) {
		t.Fatal("risk class-3 field must count")
	}
}

func TestT509MalformedFinishReportIsFlaggedNotSilent(t *testing.T) {
	raw := "```jevons\njevons: kind finish-report\njevons: target T509\n```\n\nDone."
	m, err := envelope.Parse(raw)
	if m == nil || err == nil {
		t.Fatalf("want malformed finish-report, m=%v err=%v", m, err)
	}
	s := &Server{}
	out, drop := s.applyEnvelopeControls("jv-t509-envelopes", raw)
	if drop {
		t.Fatal("malformed load-bearing must still be delivered, flagged")
	}
	if !strings.Contains(out, envelope.BannerHeading) {
		t.Fatalf("missing envelope banner:\n%s", out)
	}
	if !strings.Contains(out, "Done.") {
		t.Fatal("payload must still land")
	}
}

func TestT509StatusPingDoesNotReap(t *testing.T) {
	raw := envelope.Format(&envelope.Message{
		Kind:    envelope.KindStatusPing,
		Target:  "T509",
		Status:  envelope.ProgressInProgress,
		Payload: "Done reading the tree, still implementing.",
	})
	if LooksLikeFinishedWorkReport(raw) {
		t.Fatal("status-ping must not reap even when payload contains done")
	}
	if ClassifyCompletionReport(raw) != CompletionNoClaim {
		t.Fatalf("class=%s", ClassifyCompletionReport(raw))
	}
}

func TestT509DailyFieldWinsOverHermeticPayload(t *testing.T) {
	raw := envelope.Format(&envelope.Message{
		Kind:    envelope.KindFinishReport,
		Target:  "T509",
		SHA:     "abcdef0123456",
		Daily:   "restart-daily",
		Payload: "go test ./internal/envelope PASS",
	})
	if !HasDailyPathEvidence(raw) {
		t.Fatal("daily slot must count as T194 evidence")
	}
}

func TestT509StatusFieldWinsOverLiveInPayload(t *testing.T) {
	raw := envelope.Format(&envelope.Message{
		Kind:    envelope.KindStatusPing,
		Target:  "T509",
		Status:  envelope.ProgressInProgress,
		Payload: "the worker is live on the branch",
	})
	if got := envelope.ClassifyProgress(raw); got != envelope.ProgressInProgress {
		t.Fatalf("status=%s — envelope field wins over prose 'live'", got)
	}
}

func TestT509ChatterDropsDuplicateStatusPing(t *testing.T) {
	s := &Server{}
	msg := envelope.Format(&envelope.Message{
		Kind:    envelope.KindStatusPing,
		Target:  "T509",
		Status:  envelope.ProgressInProgress,
		Payload: "still going",
	})
	first, drop := s.applyEnvelopeControls("w", msg)
	if drop || first != msg {
		t.Fatalf("first must deliver original: drop=%v", drop)
	}
	second, drop := s.applyEnvelopeControls("w", msg)
	if drop {
		t.Fatal("first duplicate delivers a notice, not a silent drop")
	}
	if !strings.Contains(second, "[chatter]") {
		t.Fatalf("notice=%q", second)
	}
	_, drop = s.applyEnvelopeControls("w", msg)
	if !drop {
		t.Fatal("subsequent identical pings in the cycle are dropped")
	}
}

func TestT509UnenvelopedFallsBackToProse(t *testing.T) {
	if ClassifyCompletionReport("Done.") != CompletionBareDone {
		t.Fatal("unenveloped bare done")
	}
	if ClassifyCompletionReport("Done. SHA abcdef0 go test PASS") != CompletionOracleEvidence {
		t.Fatal("unenveloped oracle")
	}
}

func TestT509AckEnvelopeIsBareAck(t *testing.T) {
	raw := envelope.Format(&envelope.Message{
		Kind:    envelope.KindAck,
		Payload: "No response requested.",
	})
	if !bareAckTurnReport(raw) {
		t.Fatal("ack kind is a T502 bare ack")
	}
}

func TestT509ChatterWindowIndependentPerActor(t *testing.T) {
	tr := envelope.NewTracker()
	now := time.Now()
	msg := &envelope.Message{Kind: envelope.KindAck}
	if tr.Check("a", msg, now).Action != envelope.ActionDeliver {
		t.Fatal("a")
	}
	if tr.Check("b", msg, now).Action != envelope.ActionDeliver {
		t.Fatal("b must not share a's fingerprint window")
	}
}
