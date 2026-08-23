// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package envelope

import (
	"strings"
	"testing"
)

func validFinish() *Message {
	return &Message{
		Kind:         KindFinishReport,
		Target:       "T509",
		SHA:          "abcdef0123456",
		GateID:       "9f13c0a2",
		Verdict:      VerdictGreen,
		Status:       ProgressInProgress,
		SilentLedger: SilentLedgerEmpty,
		Payload:      "Work landed. SHA abcdef0123456.",
	}
}

func TestFormatParseRoundTrip(t *testing.T) {
	raw := Format(validFinish())
	if !strings.HasPrefix(raw, "```jevons\n") {
		t.Fatalf("fence must open at line 1 with info string jevons:\n%s", raw)
	}
	if !strings.Contains(raw, "jevons: kind finish-report") {
		t.Fatalf("missing kind slot:\n%s", raw)
	}
	if strings.HasPrefix(raw, "---") {
		t.Fatal("YAML front matter is not the envelope format")
	}
	got, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got == nil {
		t.Fatal("expected envelope")
	}
	if got.Kind != KindFinishReport || got.Target != "T509" {
		t.Fatalf("kind=%s target=%s", got.Kind, got.Target)
	}
	if got.SHA != "abcdef0123456" || got.GateID != "9f13c0a2" {
		t.Fatalf("oracle sha=%s gate-id=%s", got.SHA, got.GateID)
	}
	if got.Verdict != VerdictGreen || got.Status != ProgressInProgress {
		t.Fatalf("verdict=%s status=%s", got.Verdict, got.Status)
	}
	if !strings.Contains(got.Payload, "Work landed") {
		t.Fatalf("payload=%q", got.Payload)
	}
	if !got.AtLine1 {
		t.Fatal("fence must be at line 1")
	}
}

func TestParseUnenveloped(t *testing.T) {
	m, err := Parse("Done. SHA abcdef0. go test ./... PASS")
	if err != nil {
		t.Fatalf("unenveloped is not an error: %v", err)
	}
	if m != nil {
		t.Fatalf("unenveloped must return nil message, got %+v", m)
	}
}

func TestParseYAMLFrontMatterIsNotAnEnvelope(t *testing.T) {
	raw := "---\nkind: finish-report\ntarget: T509\n---\n\nDone."
	m, err := Parse(raw)
	if err != nil {
		t.Fatalf("YAML is unenveloped, not malformed: %v", err)
	}
	if m != nil {
		t.Fatalf("YAML front matter must not parse as a jevons envelope: %+v", m)
	}
}

func TestParseMalformedFinishReport(t *testing.T) {
	raw := "```jevons\njevons: kind finish-report\njevons: target T509\n```\n\nDone."
	m, err := Parse(raw)
	if m == nil {
		t.Fatal("malformed still returns the message so callers can flag it")
	}
	if err == nil {
		t.Fatal("finish-report without oracle or risk must be flagged")
	}
	if !strings.Contains(err.Error(), "oracle") {
		t.Fatalf("err=%v", err)
	}
	if Banner(err) == "" || !strings.Contains(Banner(err), "🎯T509") {
		t.Fatalf("banner=%q", Banner(err))
	}
}

func TestParseUnknownKind(t *testing.T) {
	raw := "```jevons\njevons: kind conlang-compact\n```\n\nhi"
	_, err := Parse(raw)
	if err == nil {
		t.Fatal("unknown kind must be flagged")
	}
}

func TestParseAfterAgentPrefix(t *testing.T) {
	inner := Format(validFinish())
	wrapped := "[Agent jv-t509-envelopes responded]\n" + inner
	got, err := Parse(wrapped)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got == nil || got.Kind != KindFinishReport {
		t.Fatalf("got %+v", got)
	}
}

func TestParseTargetCanonical(t *testing.T) {
	raw := "```jevons\njevons: kind spawn-brief\njevons: target 🎯T27.2\n```\n\nGo."
	got, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Target != "T27.2" {
		t.Fatalf("target=%q", got.Target)
	}
}

func TestParseStatusPingRequiresStatus(t *testing.T) {
	raw := "```jevons\njevons: kind status-ping\njevons: target T509\n```\n\nstill going"
	_, err := Parse(raw)
	if err == nil {
		t.Fatal("status-ping without status must be flagged")
	}
}

func TestParseAckBareKind(t *testing.T) {
	raw := "```jevons\njevons: kind ack\n```\n\nNo response requested."
	got, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Kind != KindAck {
		t.Fatalf("kind=%s", got.Kind)
	}
}

func TestParseQuotedFenceIsNotAnEnvelope(t *testing.T) {
	raw := "Earlier they wrote:\n\n```jevons\njevons: kind ack\n```\n\nthat was them."
	m, err := Parse(raw)
	if err != nil {
		t.Fatalf("quoted fence is unenveloped: %v", err)
	}
	if m != nil {
		t.Fatalf("quoted fence must not be an envelope: %+v", m)
	}
}

func TestIncompleteFenceIsUnenveloped(t *testing.T) {
	raw := "```jevons\njevons: kind finish-report\njevons: target T509\n"
	m, err := Parse(raw)
	if err != nil || m != nil {
		t.Fatalf("mid-stream incomplete fence is not malformed: m=%v err=%v", m, err)
	}
}

func TestSigilSpelledOut(t *testing.T) {
	if Sigil != "jevons:" {
		t.Fatalf("sigil=%q want jevons:", Sigil)
	}
	if strings.HasPrefix(Sigil, "jv") && Sigil != "jevons:" {
		t.Fatal("do not abbreviate the sigil")
	}
}
