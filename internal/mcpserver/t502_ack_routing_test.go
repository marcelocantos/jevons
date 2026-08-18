// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agentreport"
)

// 🎯T502: a bare "No response requested." ack is never routed past the PO to
// the overseer as owner-decision traffic.
//
// Observed live twice on 2026-08-18, post-🎯T445: jv-t445-finish-guard
// (report 20260818T050938Z-85caba2f) and jv-t399-web-green (report
// 20260818T051529Z-85caba2f) each ended a turn with the exact 22-byte ack
// "No response requested.", and the turn-completion seam (agentEventSink →
// notify) delivered it straight to the overseer — the T392.7 content router
// only guards the fleet-send path, not this one. T445 taught the reap chain
// that such a turn is not a finish; this pins that the notify seam agrees:
// an unclassified bare ack stays routine and is delivered nowhere, while
// positive finish shapes, overseer asks, and T392.7 direct-route classes
// still escalate.

// liveMisrouteAck is the exact stored text of both live misroute fixtures
// (~/.jevons/agent-reports/jv-t445-finish-guard/20260818T050938Z-85caba2f.json
// and …/jv-t399-web-green/20260818T051529Z-85caba2f.json, 22 bytes each).
const liveMisrouteAck = "No response requested."

// TestT502BareAckDoesNotReachOverseer replays the two live misroutes through
// the production report-delivery call site (🎯T419): the per-agent event sink
// whose terminal stop is what delivered them to the overseer on 2026-08-18.
func TestT502BareAckDoesNotReachOverseer(t *testing.T) {
	for _, agent := range []string{"jv-t445-finish-guard", "jv-t399-web-green"} {
		t.Run(agent, func(t *testing.T) {
			s := &Server{}
			var delivered []string
			s.SetNotify(func(text string) { delivered = append(delivered, text) })

			sink := s.agentEventSink(agent)
			sink(claudia.Event{Type: "assistant", Text: liveMisrouteAck, StopReason: "end_turn"})

			if len(delivered) != 0 {
				t.Fatalf("bare ack %q from %s reached the overseer: %q",
					liveMisrouteAck, agent, delivered)
			}
		})
	}
}

// TestT502BareAckVariantsStayRoutine covers the equivalent empty-ack shapes
// the T402 vocabulary recognises — same decision, same call site.
func TestT502BareAckVariantsStayRoutine(t *testing.T) {
	for _, ack := range []string{
		"No response requested.",
		"no response needed",
		"Acknowledged.",
		"Nothing to report.",
		"Understood.",
	} {
		s := &Server{}
		var delivered []string
		s.SetNotify(func(text string) { delivered = append(delivered, text) })

		sink := s.agentEventSink("w")
		sink(claudia.Event{Type: "assistant", Text: ack, StopReason: "end_turn"})

		if len(delivered) != 0 {
			t.Errorf("bare ack %q reached the overseer: %q", ack, delivered)
		}
	}
}

// TestT502ControlReportsStillRoute is the control arm: reports that carry
// content — a genuine needs-owner ask, an oracle-backed finish, or an
// ordinary status report — still reach the overseer through the same seam.
func TestT502ControlReportsStillRoute(t *testing.T) {
	for name, report := range map[string]string{
		"needs_owner": "Blocked: needs owner verdict on the provider spend cap before I can proceed.",
		"oracle_done": "Done: TestT445FinishShape green under bin/gate, commit e6d9b3e8.",
		"status":      "Progress: the classifier is wired; hermetic tests are next.",
		// A report that merely CONTAINS the ack phrase is a report, not an ack
		// (the T402 vocabulary is exact-match for exactly this reason).
		"ack_plus_content": "No response requested — but the build is broken, see below.",
	} {
		t.Run(name, func(t *testing.T) {
			s := &Server{}
			var delivered []string
			s.SetNotify(func(text string) { delivered = append(delivered, text) })

			sink := s.agentEventSink("jv-control")
			sink(claudia.Event{Type: "assistant", Text: report, StopReason: "end_turn"})

			if len(delivered) != 1 {
				t.Fatalf("control report %q did not reach the overseer (deliveries: %d)",
					report, len(delivered))
			}
			if !strings.Contains(delivered[0], "jv-control") {
				t.Fatalf("delivered report does not name the agent: %q", delivered[0])
			}
		})
	}
}

// TestT502SuppressedAckIsStillStored pins the 🎯T388 half: suppression is a
// routing decision, not amnesia — the ack is still stored as the agent's
// report, exactly as the two live fixtures were.
func TestT502SuppressedAckIsStillStored(t *testing.T) {
	s := &Server{}
	dir := t.TempDir()
	s.SetAgentReportDir(dir)
	s.SetNotify(func(string) {})

	sink := s.agentEventSink("jv-ack-store")
	sink(claudia.Event{Type: "assistant", Text: liveMisrouteAck, StopReason: "end_turn"})

	reports, err := agentreport.List(dir, "jv-ack-store")
	if err != nil {
		t.Fatalf("listing stored reports: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("suppressed ack was not stored: %d reports", len(reports))
	}
}
