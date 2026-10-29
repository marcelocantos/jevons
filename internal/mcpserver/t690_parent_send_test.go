// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agentreport"
	"github.com/marcelocantos/jevons/internal/envelope"
	"github.com/marcelocantos/jevons/internal/fleetlog"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

// 🎯T690 — a seat whose policy denies jevons_agent_send still delivers a
// terminal report to its registry parent on the daemon path.

const t690Worker = "jv-t690-probe"
const t690Parent = "jevons-po"

func t690ScoutReport() string {
	return envelope.Format(&envelope.Message{
		Kind:         envelope.KindScoutReport,
		Target:       "T690",
		Phase:        envelope.PhaseScout,
		SilentLedger: envelope.SilentLedgerRanked,
		Decisions: []envelope.SilentDecision{
			{Confidence: 0.4, Choice: "daemon-delivered parent report", Why: "approval policy never"},
		},
		FogKnown:   []string{"notify already stores the terminal report"},
		FogUnknown: []string{"none for this slice"},
		Payload:    "Scout complete — parent must receive this without jevons_agent_send.",
	})
}

func t690Server(t *testing.T) (*Server, *fakeSender, *overseerInbox, string) {
	t.Helper()
	po := &fakeSender{alive: true}
	s, inbox := chainServer(t, map[string]*fakeSender{t690Parent: po})
	s.registry = newLineageRegistry(t, map[string]string{
		t690Parent: "jevons",
		t690Worker: t690Parent,
	})
	reportDir := t.TempDir()
	s.SetAgentReportDir(reportDir)
	s.DenyAgentSend(t690Worker)
	return s, po, inbox, reportDir
}

func t690Send(t *testing.T, s *Server, actor, dest, text string) *mcp.CallToolResult {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": dest, "text": text, "actor": actor}
	res, err := s.handleAgentSend(context.Background(), req)
	if err != nil {
		t.Fatalf("handleAgentSend: %v", err)
	}
	return res
}

// TestT690DeniedSendStillDeliversTerminalReportToParent is the acceptance
// hermetic: mint a seat whose policy denies jevons_agent_send, have it
// produce a terminal report, assert the parent receives it.
func TestT690DeniedSendStillDeliversTerminalReportToParent(t *testing.T) {
	s, po, inbox, reportDir := t690Server(t)
	report := t690ScoutReport()

	denied := t690Send(t, s, t690Worker, t690Parent, report)
	if !denied.IsError {
		t.Fatalf("jevons_agent_send must be denied under approval-never; got %q", toolText(denied))
	}
	if !strings.Contains(toolText(denied), "approval policy is never") {
		t.Fatalf("deny text=%q want specimen %q", toolText(denied), ErrApprovalPolicyNever)
	}
	if len(po.sent) != 0 {
		t.Fatalf("denied send leaked onto parent: %v", po.sent)
	}

	sink := s.agentEventSink(t690Worker)
	sink(claudia.Event{Type: "assistant", Text: report, StopReason: "end_turn"})

	if len(po.sent) != 1 {
		t.Fatalf("parent deliveries=%d want 1 (daemon parent-report); got %v", len(po.sent), po.sent)
	}
	if !strings.Contains(po.sent[0], envelope.SeatFact(envelope.IntentRoute, t690Worker)) {
		t.Fatalf("parent copy lost agent framing: %q", po.sent[0])
	}
	if !strings.Contains(po.sent[0], "parent must receive this without jevons_agent_send") {
		t.Fatalf("parent copy lost scout payload: %q", po.sent[0])
	}
	if !strings.Contains(po.sent[0], "scout-report") {
		t.Fatalf("parent copy lost scout-report envelope: %q", po.sent[0])
	}

	got, err := agentreport.Latest(reportDir, t690Worker)
	if err != nil {
		t.Fatalf("stored report: %v", err)
	}
	if !strings.Contains(got.Text, "parent must receive this without jevons_agent_send") {
		t.Fatalf("stored report missing payload: %q", got.Text)
	}

	// The parent is a running product owner, which acts on the report, so
	// the overseer gets no second copy (2026-10-23 overseer turn burn).
	if len(inbox.texts) != 0 {
		t.Fatalf("overseer deliveries=%d want 0 when the parent PO is running: %v", len(inbox.texts), inbox.texts)
	}
}

func TestT690StartResultCitesDaemonParentReport(t *testing.T) {
	msg := formatAgentStartResult(t690Worker, "/tmp/w", t690Parent, "work", "worker", "T690", "grok", "", "sess", "", "")
	if !strings.Contains(msg, ParentReportChannelCite) {
		t.Fatalf("spawn result missing parent-report channel: %q", msg)
	}
	if !strings.Contains(msg, "not gated on jevons_agent_send approval") {
		t.Fatalf("spawn result does not say the channel is ungated: %q", msg)
	}
}

func TestT690BareAckDoesNotReachParent(t *testing.T) {
	s, po, inbox, _ := t690Server(t)
	sink := s.agentEventSink(t690Worker)
	sink(claudia.Event{Type: "assistant", Text: liveMisrouteAck, StopReason: "end_turn"})
	if len(po.sent) != 0 {
		t.Fatalf("bare ack reached parent: %v", po.sent)
	}
	if len(inbox.texts) != 0 {
		t.Fatalf("bare ack reached overseer: %v", inbox.texts)
	}
}

func TestT690OverseerParentIsNotDoubleDelivered(t *testing.T) {
	s, inbox := chainServer(t, nil)
	s.registry = newLineageRegistry(t, map[string]string{
		"leaf-w": "jevons",
	})
	s.SetAgentReportDir(t.TempDir())
	sink := s.agentEventSink("leaf-w")
	sink(claudia.Event{
		Type:       "assistant",
		Text:       "Done. SHA abcdef0123456. hermetic TestT690 PASS",
		StopReason: "end_turn",
	})
	if len(inbox.texts) != 1 {
		t.Fatalf("overseer-parented worker deliveries=%d want 1: %v", len(inbox.texts), inbox.texts)
	}
}

// A finish-report reaps the worker before its last turn-end notify runs, so
// the registry row is gone when the report arrives. The parent recorded on
// the removal still decides that a running PO owns it: the overseer gets no
// copy (2026-10-27: jv-t1229-parked-ambient's finish cost an overseer turn).
func TestT690ReapedWorkerUnderRunningPOSkipsOverseerCopy(t *testing.T) {
	s, _, inbox, _ := t690Server(t)
	s.SetRemovalAccount(fleetlog.New(nil))
	sink := s.agentEventSink(t690Worker)
	ok, err := s.RemovalAccount().Remove(s.registry, t690Worker, fleetlog.Removal{
		Reason: fleetlog.ReasonReapDone,
		Detail: "reaped on a finished-work report (finished_work)",
	})
	if err != nil || !ok {
		t.Fatalf("reap: ok=%v err=%v", ok, err)
	}
	if s.registry.Def(t690Worker) != nil {
		t.Fatal("worker still registered after reap")
	}
	sink(claudia.Event{Type: "assistant", Text: t690ScoutReport(), StopReason: "end_turn"})
	if len(inbox.texts) != 0 {
		t.Fatalf("overseer got %d copies of a reaped PO worker's report: %v", len(inbox.texts), inbox.texts)
	}
}

// The overseer copy remains the net when the parent PO is known to be down.
func TestT690DeadParentPOStillCopiesOverseer(t *testing.T) {
	s, po, inbox, _ := t690Server(t)
	po.alive = false
	s.Seats().Observe(seatstate.Observation{
		Name: t690Parent, Alive: seatstate.No, LocalAlive: seatstate.No,
		QueueDepth: seatstate.QueueUnknown, Source: "test",
	})
	if got := s.seatState(t690Parent).Alive; got != seatstate.No {
		t.Fatalf("fixture: parent liveness = %v, want No", got)
	}
	sink := s.agentEventSink(t690Worker)
	sink(claudia.Event{Type: "assistant", Text: t690ScoutReport(), StopReason: "end_turn"})
	if len(inbox.texts) != 1 {
		t.Fatalf("overseer deliveries=%d want 1 when the parent PO is down: %v", len(inbox.texts), inbox.texts)
	}
}
