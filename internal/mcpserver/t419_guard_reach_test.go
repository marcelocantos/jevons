// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/delivery"
)

// TestT419DeliveryEvidenceReachedThroughBegunSend is the living pinning
// instance of 🎯T419: delivery.RecordPayload is reached through the production
// send path (deliverByName → reportSendOutcome → recordDeliveryEvidence)
// under the predicate that gates it — OutcomeBegun (turn-begin) — not by
// calling RecordPayload directly.
//
// A green TestRecordAndLookupSurviveCompactionShapedWipe proves the store
// works when called. This test proves the production caller reaches it when
// a large send becomes a turn. That is the distinction 🎯T419 acceptance 1
// requires, and the shape the 2026-08-10 MarkDelivered unit test lacked.
func TestT419DeliveryEvidenceReachedThroughBegunSend(t *testing.T) {
	tr := newSessionLog(t)
	agent := &sendingAgent{alive: true, onPaste: func(text string) { tr.submitted(text) }}
	s, _ := sendFixture(t, agent, tr)

	stateDir := t.TempDir()
	s.SetAgentReportDir(stateDir)

	payload := strings.Repeat(brief, 20) // multi-KB: the size that stuck live
	const name = "jv-t419-worker"
	res, err := s.deliverByName(name, payload, OriginAgent, false)
	if err != nil {
		t.Fatalf("begun send must not error: %v", err)
	}
	if res.Status != "sent" {
		t.Fatalf("status=%q want sent (OutcomeBegun path)", res.Status)
	}

	// Production path: durable evidence must exist without a direct RecordPayload call.
	ok, err := delivery.WasDelivered(stateDir, name, payload)
	if err != nil {
		t.Fatalf("WasDelivered: %v", err)
	}
	if !ok {
		t.Fatal("delivery.RecordPayload was not reached through reportSendOutcome on OutcomeBegun — " +
			"the guard works when called (store unit tests) but did not run on the production path (🎯T419)")
	}

	// Negative control under the gating predicate: an unsubmitted paste must
	// NOT record evidence. If it did, the store would attest a delivery the
	// production caller correctly refused — the inverted form of the original defect.
	tr2 := newSessionLog(t)
	tr2.submitted("earlier work that DID land")
	agent2 := &sendingAgent{alive: true} // pastes, never submits
	s2, _ := sendFixture(t, agent2, tr2)
	s2.SetAgentReportDir(stateDir)
	s2.noteTurnEnded("jv-t419-stuck")
	stuckPayload := strings.Repeat(brief, 12) + "\nT419-negative-control-unsubmitted"
	if _, err := s2.deliverByName("jv-t419-stuck", stuckPayload, OriginAgent, false); err == nil {
		t.Fatal("unsubmitted send must fail closed")
	}
	if ok, err := delivery.WasDelivered(stateDir, "jv-t419-stuck", stuckPayload); err != nil {
		t.Fatalf("WasDelivered stuck: %v", err)
	} else if ok {
		t.Fatal("unsubmitted path recorded delivery evidence — predicate OutcomeBegun was not gating the guard")
	}
}
