// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/envelope"
	"github.com/mark3labs/mcp-go/mcp"
)

const (
	t1233PO     = "jevons-po"
	t1233Child  = "jv-t1233-worker"
	t1233Target = "T1233"
)

func declaredBlockedFinishReport(target, blocker, payload string) string {
	return envelope.Format(&envelope.Message{
		Kind:         envelope.KindFinishReport,
		Target:       target,
		Status:       envelope.ProgressBlocked,
		Blocker:      blocker,
		SilentLedger: envelope.SilentLedgerEmpty,
		Payload:      payload,
	})
}

func t1233Server(t *testing.T) (*Server, *fakeSender, *fakeSender) {
	t.Helper()
	po := &fakeSender{alive: true}
	child := &fakeSender{alive: true}
	s, _ := chainServer(t, map[string]*fakeSender{
		t1233PO:    po,
		t1233Child: child,
	})
	s.registry = newLineageRegistry(t, map[string]string{
		t1233PO:    "jevons",
		t1233Child: t1233PO,
	})
	s.SetAgentReportDir(t.TempDir())
	return s, po, child
}

func t1233WakePO(t *testing.T, s *Server, po *fakeSender, report string) {
	t.Helper()
	sink := s.agentEventSink(t1233Child)
	sink(claudia.Event{Type: "assistant", Text: report, StopReason: "end_turn"})
	if len(po.sent) == 0 {
		t.Fatal("blocked child report must reach the PO")
	}
}

func t1233EndPOTurn(s *Server, po *fakeSender) {
	po.inFlight = false
	s.noteTurnEnded(t1233PO)
}

func TestT1233POTurnEndFlagsUndeliveredDirection(t *testing.T) {
	s, po, child := t1233Server(t)
	report := declaredBlockedFinishReport(t1233Target, "needs parent direction on flake retry", "blocked; waiting on PO")
	t1233WakePO(t, s, po, report)
	before := len(po.sent)
	t1233EndPOTurn(s, po)
	if len(po.sent) != before+1 {
		t.Fatalf("PO deliveries=%d want %d (one undelivered-direction flag)", len(po.sent), before+1)
	}
	flag := po.sent[len(po.sent)-1]
	if !strings.Contains(flag, "your direction to "+t1233Child+" was not delivered") {
		t.Fatalf("flag missing exact phrase: %q", flag)
	}
	if !strings.Contains(flag, "🎯T1233") {
		t.Fatalf("flag missing target cite: %q", flag)
	}
	if len(child.sent) != 0 {
		t.Fatalf("child must receive zero deliveries, got %v", child.sent)
	}

	list, err := observedAgentList(s, context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	body := toolText(list)
	if !strings.Contains(body, AgentStatusAwaitingParentDirection) {
		t.Fatalf("agent_list missing awaiting_parent_direction:\n%s", body)
	}
	if !strings.Contains(body, "awaiting parent direction") {
		t.Fatalf("agent_list missing annotation:\n%s", body)
	}

	// Once: a second PO turn without a send must not re-flag.
	t1233EndPOTurn(s, po)
	if len(po.sent) != before+1 {
		t.Fatalf("second turn re-flagged: deliveries=%d", len(po.sent))
	}
}

func TestT1233POSendSuppressesFlag(t *testing.T) {
	s, po, child := t1233Server(t)
	report := declaredBlockedFinishReport(t1233Target, "needs parent direction", "blocked")
	t1233WakePO(t, s, po, report)
	before := len(po.sent)

	child.inFlight = false
	s.noteTurnEnded(t1233Child)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":  t1233Child,
		"text":  "retry the flake with the pinned seed",
		"actor": t1233PO,
	}
	res, err := s.handleAgentSend(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("PO send: %s", toolText(res))
	}
	if len(child.sent) == 0 {
		t.Fatal("child must receive the PO direction")
	}

	t1233EndPOTurn(s, po)
	for _, msg := range po.sent[before:] {
		if strings.Contains(msg, "your direction to") {
			t.Fatalf("send during turn must not flag: %q", msg)
		}
	}
	list, err := observedAgentList(s, context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(toolText(list), AgentStatusAwaitingParentDirection) {
		t.Fatalf("status lingered after send:\n%s", toolText(list))
	}
}

func TestT1233MidTurnReportDoesNotFlagCurrentTurn(t *testing.T) {
	s, po, child := t1233Server(t)
	// Start a PO turn with an unrelated send so the blocked report arrives mid-turn.
	if _, err := s.deliverByName(t1233PO, "unrelated owner note", OriginOwner, false); err != nil {
		t.Fatal(err)
	}
	if s.flightState(t1233PO) != FlightInFlight {
		t.Fatalf("PO flight=%s want in_flight", s.flightState(t1233PO))
	}
	po.inFlight = true
	report := declaredBlockedFinishReport(t1233Target, "needs parent direction", "blocked mid-turn")
	sink := s.agentEventSink(t1233Child)
	sink(claudia.Event{Type: "assistant", Text: report, StopReason: "end_turn"})
	before := len(po.sent)
	t1233EndPOTurn(s, po)
	for _, msg := range po.sent[before:] {
		if strings.Contains(msg, "your direction to") {
			t.Fatalf("mid-turn report must not flag the current turn: %q", msg)
		}
	}
	if len(child.sent) != 0 {
		t.Fatalf("child deliveries=%v", child.sent)
	}

	// The next PO turn (woken by the queued report, or started empty) must flag.
	if _, err := s.deliverByName(t1233PO, "next turn", OriginOwner, false); err != nil {
		t.Fatal(err)
	}
	before = len(po.sent)
	t1233EndPOTurn(s, po)
	found := false
	for _, msg := range po.sent[before:] {
		if strings.Contains(msg, "your direction to "+t1233Child+" was not delivered") {
			found = true
		}
	}
	if !found {
		t.Fatalf("next turn must flag; PO inbox=%v", po.sent[before:])
	}
}
