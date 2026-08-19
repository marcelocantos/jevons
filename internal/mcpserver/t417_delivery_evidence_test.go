// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/delivery"
	"github.com/marcelocantos/jevons/internal/turnev"
)

// 🎯T417 clause 2: a confirmed delivery stays provable after a compaction-
// shaped wipe of the receiving transcript.
func TestT417DeliveryEvidenceSurvivesTranscriptWipe(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	const agent = "jv-t417-worker"
	if err := reg.Register(claudia.AgentDef{
		Name: agent, WorkDir: dir, SessionID: "sess-t417",
		Purpose: claudia.PurposeWork, Parent: "jevons-po", Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.SetAgentReportDir(dir)

	payload := "endorsement: please continue with clause 2 of 🎯T417 — distinctive needle text."
	ev := TurnEvidence{ConversationGrew: true, Detail: "transcript grew 0→900 bytes"}
	s.recordDeliveryEvidence(agent, payload, ev)

	// Compaction-shaped wipe: session transcript reduced to a handover seed.
	sess := filepath.Join(dir, "sessions", "sess-t417.jsonl")
	if err := os.MkdirAll(filepath.Dir(sess), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sess, []byte(`{"type":"user","message":{"content":"handover only"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !s.priorDelivery(agent, payload) {
		t.Fatal("prior delivery evidence vanished after transcript wipe")
	}
	if got := s.ReadingForPayload(agent, payload, turnev.FateUnseen); got != turnev.ReadingDelivered {
		t.Fatalf("ReadingForPayload=%s want delivered (durable evidence)", got)
	}
	outcome := s.classifySend(agent, payload, FlightIdle, TurnEvidence{})
	if outcome != OutcomeBegun {
		t.Fatalf("classifySend after wipe=%s want begun", outcome)
	}
	ok, err := delivery.WasDelivered(dir, agent, payload)
	if err != nil || !ok {
		t.Fatalf("store WasDelivered ok=%v err=%v", ok, err)
	}
}

// 🎯T417 clause 3: mid-turn absence must not classify as not-submitted.
func TestT417MidTurnAbsenceIsUnconfirmedNotStuck(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, nil, nil)
	const agent = "jv-t417-composing"
	s.noteTurnInFlight(agent)

	payload := "a payload long enough that Needle will identify it for mid-turn honesty"
	// Observed+Durable+idle absence is the idle stuck verdict; composing must
	// demote it so a mid-turn read never false-negatives as not_submitted.
	absent := TurnEvidence{
		Observed: true,
		Durable:  true,
		Detail:   "payload absent from user messages and queue records",
	}
	if got := ClassifySendOutcome(FlightIdle, absent); got != OutcomeNotSubmitted {
		t.Fatalf("precondition: idle ClassifySendOutcome=%s want not_submitted", got)
	}
	outcome := s.classifySend(agent, payload, FlightIdle, absent)
	if outcome != OutcomeUnconfirmed {
		t.Fatalf("mid-turn classifySend=%s want unconfirmed (false-negative ban)", outcome)
	}
	if got := s.ReadingForPayload(agent, payload, turnev.FateUnseen); got != turnev.ReadingUndecided {
		t.Fatalf("ReadingForPayload mid-turn=%s want undecided", got)
	}
}
