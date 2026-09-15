// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/agentreport"
	"github.com/marcelocantos/jevons/internal/eventlog"
	"github.com/marcelocantos/jevons/internal/relayroute"
	"github.com/marcelocantos/jevons/internal/roles"
)

// 🎯T658 — the 2026-09-15 reroute, replayed on the product send path.
//
// jv-t657-steer-ui's first jevons_agent_send to jevons-po was its
// scout-report. handleAgentSend wrapped that first send in the identity
// header, the standing brief and the product-owner doctrine; the relay
// stripped only the brief, so the doctrine and the fence were scanned as one
// prose report and "oracle" + "Scout done" made it oracle_done. The PO got a
// record line whose summary was the doctrine (🎯T614's symptom) and, on
// following it, "no stored reports".

func t658Fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "relayroute", "testdata", "t658", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// t658Server is chainServer plus what the incident path needs: a registry
// whose jevons-po resolves to the product-owner doctrine, an event journal,
// and NO prior brief to jevons-po, so the first send wraps.
func t658Server(t *testing.T) (*Server, *fakeSender, *overseerInbox, string) {
	t.Helper()
	po := &fakeSender{alive: true}
	s, inbox := chainServer(t, map[string]*fakeSender{"jevons-po": po})
	s.registry = newLineageRegistry(t, map[string]string{
		"jevons-po":        "jevons",
		"jv-t657-steer-ui": "jevons-po",
	})
	logPath := filepath.Join(t.TempDir(), "logs", "events.jsonl")
	journal, err := eventlog.Open(logPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	s.SetEventJournal(journal)
	s.SetAgentReportDir(t.TempDir())
	return s, po, inbox, logPath
}

func t658Send(t *testing.T, s *Server, actor, text string) *mcp.CallToolResult {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "jevons-po", "text": text, "actor": actor}
	res, err := s.handleAgentSend(context.Background(), req)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if res.IsError {
		t.Fatalf("send: %s", toolText(res))
	}
	return res
}

func t658RelayEvents(t *testing.T, logPath, worker string) []eventlog.Event {
	t.Helper()
	events, err := eventlog.Tail(logPath, eventlog.TailOptions{
		Component: "relayroute", Decision: "overseer", Contains: worker,
	})
	if err != nil {
		t.Fatal(err)
	}
	return events
}

// Acceptance 1, on the wire: the worker's actual first send (its scout
// report), wrapped exactly as the product wraps a first send, stays on the
// PO. No overseer copy, no record line, no relayroute event.
func TestT658FirstSendScoutReportStaysOnPO(t *testing.T) {
	s, po, inbox, logPath := t658Server(t)
	scout := t658Fixture(t, "t657-scout-report.txt")

	res := t658Send(t, s, "jv-t657-steer-ui", scout)
	if !strings.Contains(toolText(res), "standing fleet brief") {
		t.Fatalf("fixture must be a first send (brief injected): %s", toolText(res))
	}
	if len(inbox.texts) != 0 {
		t.Fatalf("scout report leaked to the overseer: %v", inbox.texts)
	}
	if len(po.sent) != 1 {
		t.Fatalf("PO inbox=%d want the one wrapped scout report", len(po.sent))
	}
	got := po.sent[0]
	if !strings.Contains(got, roles.DoctrineMarker) || !strings.Contains(got, "jevons: kind scout-report") {
		t.Fatalf("PO must receive the wrapped scout report; got %q", got)
	}
	if strings.Contains(got, "routed to overseer") || strings.Contains(strings.ToLower(got), "oracle_done") {
		t.Fatalf("PO received a skipped-hop record for a scout report: %q", got)
	}
	if ev := t658RelayEvents(t, logPath, "jv-t657-steer-ui"); len(ev) != 0 {
		t.Fatalf("relayroute events=%d want none: %v", len(ev), ev)
	}

	// The relay reads the sender's words, not the doctrine: with the role
	// body the wrap injected, the report body IS the scout envelope.
	body := relayReportBodyWithRole(got, s.roleBodyForAgent("jevons-po"))
	if !strings.HasPrefix(body, "```jevons\n") {
		t.Fatalf("relay report body must open at the sender's fence; got %.120q", body)
	}
	if relayroute.ReportSummary(body) == relayroute.ReportSummary(got) {
		t.Fatal("summary of the body equals summary of the wrap — the doctrine is still ahead of the report")
	}
	// And without the role body the doctrine cannot be bounded: the relay
	// says so (empty ⇒ parent) instead of scanning the doctrine.
	if unbounded := relayReportBody(got); unbounded != "" {
		t.Fatalf("unbounded doctrine must yield an empty report, got %.120q", unbounded)
	}
}

// Acceptance 1, the openers: the jv-t657 turn-3 line and the jv-t658
// "Proceeding…" line, each as a worker's own send, stay on the PO whether
// or not the send is the first one (wrapped) to that PO.
func TestT658ProgressOpenersStayOnPOOnTheWire(t *testing.T) {
	for _, name := range []string{"t657-opener.txt", "t658-proceeding.txt"} {
		t.Run(name, func(t *testing.T) {
			s, po, inbox, logPath := t658Server(t)
			opener := t658Fixture(t, name)
			t658Send(t, s, "jv-t657-steer-ui", opener) // first send: wrapped
			po.inFlight = false
			s.noteTurnEnded("jevons-po")
			t658Send(t, s, "jv-t657-steer-ui", opener) // second: bare
			if len(inbox.texts) != 0 {
				t.Fatalf("opener leaked to the overseer: %v", inbox.texts)
			}
			if len(po.sent) != 2 {
				t.Fatalf("PO inbox=%d want two deliveries", len(po.sent))
			}
			for _, got := range po.sent {
				if !strings.Contains(got, strings.TrimSpace(opener)) || strings.Contains(got, "routed to overseer") {
					t.Fatalf("PO must receive the opener itself, not a record: %q", got)
				}
			}
			if ev := t658RelayEvents(t, logPath, "jv-t657-steer-ui"); len(ev) != 0 {
				t.Fatalf("relayroute events=%d want none", len(ev))
			}
		})
	}
}

// Acceptance 2: a record line is only ever emitted over a stored report.
// With no report store there is nothing for jevons_agent_report_read to
// answer with, so the relay does not skip the hop — the PO gets the full
// report as before. With a store, the reroute stores the sender's words
// (not the wrap) and the record line summarizes those same words.
func TestT658SkippedHopRecordRequiresStoredReport(t *testing.T) {
	report := "Blocked: needs owner verdict on the provider spend cap before I can proceed."

	t.Run("no store, no record", func(t *testing.T) {
		po := &fakeSender{alive: true}
		s, inbox := chainServer(t, map[string]*fakeSender{"jevons-po": po})
		s.registry = newLineageRegistry(t, map[string]string{
			"jevons-po": "jevons",
			"jv-t10":    "jevons-po",
		})
		s.fleetBriefed = map[string]bool{"jevons-po": true}
		if s.agentReportStateDir() != "" {
			t.Fatal("precondition: no report store")
		}
		res := t658Send(t, s, "jv-t10", report)
		if strings.Contains(toolText(res), "rerouted") {
			t.Fatalf("reroute without a stored report: %s", toolText(res))
		}
		if len(inbox.texts) != 0 {
			t.Fatalf("overseer inbox=%v want none", inbox.texts)
		}
		if len(po.sent) != 1 || !strings.Contains(po.sent[0], report) || strings.Contains(po.sent[0], "routed to overseer") {
			t.Fatalf("PO inbox=%v want the full report, no record", po.sent)
		}
	})

	t.Run("store, record over the stored report", func(t *testing.T) {
		s, po, inbox, logPath := t658Server(t)
		res := t658Send(t, s, "jv-t657-steer-ui", report) // first send: wrapped
		if !strings.Contains(toolText(res), "rerouted") {
			t.Fatalf("needs_owner report must skip the hop: %s", toolText(res))
		}
		if len(inbox.texts) != 1 || !strings.Contains(inbox.texts[0], report) {
			t.Fatalf("overseer inbox=%v want the full report", inbox.texts)
		}
		if len(po.sent) != 1 || !strings.Contains(po.sent[0], "routed to overseer") {
			t.Fatalf("PO inbox=%v want one record line", po.sent)
		}
		record := po.sent[0]
		if !strings.Contains(record, "Blocked: needs owner verdict") || strings.Contains(record, roles.DoctrineMarker) || strings.Contains(record, "Product-owner role") {
			t.Fatalf("record must summarize the report, not the doctrine: %q", record)
		}
		stored, err := agentreport.Latest(s.agentReportStateDir(), "jv-t657-steer-ui")
		if err != nil {
			t.Fatalf("jevons_agent_report_read must find the report the record points at: %v", err)
		}
		if strings.TrimSpace(stored.Text) != report {
			t.Fatalf("stored report=%q want the sender's words", stored.Text)
		}
		ev := t658RelayEvents(t, logPath, "jv-t657-steer-ui")
		if len(ev) != 1 || ev[0].Fields["summary"] != relayroute.ReportSummary(report) {
			t.Fatalf("relayroute events=%v want one with the report summary", ev)
		}
	})
}
