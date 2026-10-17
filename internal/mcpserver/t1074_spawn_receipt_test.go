// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/claudiawire"
	"github.com/marcelocantos/jevons/internal/sendq"
)

// TestT1074SpawnSettledReceiptIgnoresTranscriptAbsence drives the opening
// brief through deliverStartPrompt. The watch looks and does not see the
// payload. The receipt completes, so the brief begins a turn and the empty
// transcript does not release the seat.
func TestT1074SpawnSettledReceiptIgnoresTranscriptAbsence(t *testing.T) {
	f := newBindFixture(t, "T1074")
	f.s.stateDir = t.TempDir()
	receiptJournal(t, f.s, t.TempDir())
	f.s.SetTurnWitness(transcriptMiss())
	const prompt = "Execute the opening brief from the receipt."
	sent, transportID := captureSpawnHandoff(t, f, prompt)
	f.s.SetSendReceiptQuery(func(_ context.Context, q claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error) {
		if transportID() == "" || q.RequestID != transportID() {
			t.Fatalf("queried %q, handoff %q", q.RequestID, transportID())
		}
		return heldReceipt(q, claudiawire.ReceiptCompleted), nil
	})
	if err := f.s.deliverStartPrompt(f.name, prompt); err != nil {
		t.Fatalf("settled opening brief: %v", err)
	}
	if !strings.Contains(sent(), prompt) {
		t.Fatalf("brief not handed off: %q", sent())
	}
	if !f.s.agentHasTurnBegan(f.name) {
		t.Fatal("settled opening brief did not begin the turn")
	}
	if f.s.priorDelivery(f.name, sent()) {
		t.Fatal("transcript absence recorded delivery")
	}
	entries, err := f.s.sendQueue().Snapshot(f.name)
	if err != nil || len(entries) != 0 {
		t.Fatalf("settled brief left the queue: %+v %v", entries, err)
	}
	if f.s.registry.Def(f.name) == nil {
		t.Fatal("settled brief released the seat")
	}
}

// TestT1074SpawnPayloadSeenDoesNotConfirmUnknownReceipt is the same opening
// brief while the witness reports PayloadSeen and the receipt is unknown.
// That hit used to make ConfirmTurnBegan accept the brief. The seat stays,
// the turn has not begun, and the attempt stays uncertain.
func TestT1074SpawnPayloadSeenDoesNotConfirmUnknownReceipt(t *testing.T) {
	f := newBindFixture(t, "T1074")
	f.s.stateDir = t.TempDir()
	const prompt = "Execute the opening brief the transcript must not accept."
	sent, transportID := captureSpawnHandoff(t, f, prompt)
	f.s.SetSendReceiptQuery(func(_ context.Context, q claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error) {
		if transportID() == "" || q.RequestID != transportID() {
			t.Fatalf("queried %q, handoff %q", q.RequestID, transportID())
		}
		r := heldReceipt(q, claudiawire.ReceiptUnknown)
		r.Reason = "not_found"
		r.Cause = ""
		return r, nil
	})
	err := f.s.deliverStartPrompt(f.name, prompt)
	if err == nil || !BriefInFlight(err) {
		t.Fatalf("PayloadSeen accepted an unsettled brief: %v", err)
	}
	if !strings.Contains(sent(), prompt) {
		t.Fatalf("brief not handed off: %q", sent())
	}
	if f.s.agentHasTurnBegan(f.name) {
		t.Fatal("unsettled brief marked the turn begun")
	}
	if f.s.priorDelivery(f.name, sent()) {
		t.Fatal("PayloadSeen recorded delivery for an unsettled brief")
	}
	released, kept := f.s.startBriefFailureTeardown(f.name, false, err)
	if released || !kept {
		t.Fatalf("teardown fork = released=%v kept=%v, want kept", released, kept)
	}
	if f.s.registry.Def(f.name) == nil {
		t.Fatal("unsettled brief reaped the seat")
	}
	entries, qerr := f.s.sendQueue().Snapshot(f.name)
	if qerr != nil || len(entries) != 1 || entries[0].State != sendq.Uncertain || entries[0].TransportRequestID != transportID() {
		t.Fatalf("unknown receipt confirmed: %+v %v", entries, qerr)
	}
}

// TestT1074SpawnTimeoutSettlesFromReceipt is an opening brief whose transport
// reports a reply timeout. The receipt completes. The empty transcript does
// not release the seat, and the timeout is not returned as a failed brief.
func TestT1074SpawnTimeoutSettlesFromReceipt(t *testing.T) {
	f := newBindFixture(t, "T1074")
	f.s.stateDir = t.TempDir()
	receiptJournal(t, f.s, t.TempDir())
	f.s.SetTurnWitness(transcriptMiss())
	f.probe.fail = errors.New("operation timed out")
	const prompt = "Execute the opening brief after a reply timeout."
	sent, transportID := captureSpawnHandoff(t, f, prompt)
	f.s.SetSendReceiptQuery(func(_ context.Context, q claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error) {
		if transportID() == "" || q.RequestID != transportID() {
			t.Fatalf("queried %q, handoff %q", q.RequestID, transportID())
		}
		return heldReceipt(q, claudiawire.ReceiptCompleted), nil
	})
	if err := f.s.deliverStartPrompt(f.name, prompt); err != nil {
		t.Fatalf("timeout with a settled receipt: %v", err)
	}
	if !strings.Contains(sent(), prompt) {
		t.Fatalf("brief not handed off: %q", sent())
	}
	if !f.s.agentHasTurnBegan(f.name) {
		t.Fatal("settled timeout did not begin the turn")
	}
	if f.s.priorDelivery(f.name, sent()) {
		t.Fatal("timeout path recorded transcript delivery")
	}
	entries, err := f.s.sendQueue().Snapshot(f.name)
	if err != nil || len(entries) != 0 {
		t.Fatalf("settled timeout left the queue: %+v %v", entries, err)
	}
}

func captureSpawnHandoff(t *testing.T, f *bindFixture, prompt string) (sent func() string, transportID func() string) {
	t.Helper()
	var gotText, gotID string
	f.probe.check = func(text, id string) {
		if !strings.Contains(text, prompt) {
			t.Fatalf("sent %q", text)
		}
		gotText = text
		gotID = id
	}
	return func() string { return gotText }, func() string { return gotID }
}
