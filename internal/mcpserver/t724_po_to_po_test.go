// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/envelope"
)

// 🎯T724: PO-to-PO sends are delivered to the addressee. On 2026-09-21
// ge-po→jevons-po defect writeups were classified as worker reports skipping
// the PO hop; the overseer got the body and both POs got a one-line reroute
// with delivered_unconfirmed — the same failure in the opposite direction.
func TestT724PoToPoDeliveredNotRerouted(t *testing.T) {
	gePO := &fakeSender{alive: true}
	jevonsPO := &fakeSender{alive: true}
	s, inbox := chainServer(t, map[string]*fakeSender{
		"ge-po":     gePO,
		"jevons-po": jevonsPO,
	})
	s.registry = newLineageRegistry(t, map[string]string{
		"ge":        "",
		"ge-po":     "ge",
		"jevons":    "",
		"jevons-po": "jevons",
	})
	s.fleetBriefed = map[string]bool{"jevons-po": true, "ge-po": true}

	text := envelope.Format(&envelope.Message{
		Kind:    envelope.KindEscalation,
		Target:  "T724",
		Payload: "Defect handoff: report_id=20260921-ge-1 gate_id=abc123 — needs owner verdict on cross-repo publish.",
	})
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":  "jevons-po",
		"text":  text,
		"actor": "ge-po",
	}
	res, err := s.handleAgentSend(context.Background(), req)
	if err != nil {
		t.Fatalf("ge-po→jevons-po: %v", err)
	}
	if res.IsError {
		t.Fatalf("ge-po→jevons-po: %s", toolText(res))
	}
	if len(jevonsPO.sent) != 1 || !strings.Contains(jevonsPO.sent[0], "report_id=20260921-ge-1") {
		t.Fatalf("addressee PO must receive the full message: %v", jevonsPO.sent)
	}
	if strings.Contains(jevonsPO.sent[0], "routed to overseer") {
		t.Fatalf("PO-to-PO must not be summarized as a skipped hop: %q", jevonsPO.sent[0])
	}
	if len(inbox.texts) != 0 {
		t.Fatalf("PO-to-PO leaked to overseer: %v", inbox.texts)
	}
	if len(gePO.sent) != 0 {
		t.Fatalf("sender PO should not receive a reroute record: %v", gePO.sent)
	}
	if got := toolText(res); strings.Contains(got, "rerouted") || strings.Contains(got, "NOT confirmed") {
		t.Fatalf("sender must not be told the message was undelivered: %q", got)
	}
}

// Reverse direction: jevons-po replying to ge-po with an oracle-shaped finish
// report is still peer traffic, not a worker hop-skip.
func TestT724PoToPoOracleFinishDelivered(t *testing.T) {
	gePO := &fakeSender{alive: true}
	jevonsPO := &fakeSender{alive: true}
	s, inbox := chainServer(t, map[string]*fakeSender{
		"ge-po":     gePO,
		"jevons-po": jevonsPO,
	})
	s.registry = newLineageRegistry(t, map[string]string{
		"ge":        "",
		"ge-po":     "ge",
		"jevons":    "",
		"jevons-po": "jevons",
	})

	text := envelope.Format(&envelope.Message{
		Kind:      envelope.KindFinishReport,
		Target:    "T711",
		SilentLedger: envelope.SilentLedgerEmpty,
		SHA: "6e9da8f5a09678a78944e871b82203acbb177522",
		Payload:   "Interrupt path fixed; GATE 45acfeb7 GREEN.",
	})
	res, err := s.deliverByNameAs("jevons-po", "ge-po", text, OriginAgent, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(gePO.sent) != 1 || !strings.Contains(gePO.sent[0], "45acfeb7") {
		t.Fatalf("ge-po inbox=%v want full finish report", gePO.sent)
	}
	if len(inbox.texts) != 0 || len(jevonsPO.sent) != 0 {
		t.Fatalf("must not reroute: overseer=%v jevons-po=%v", inbox.texts, jevonsPO.sent)
	}
	if strings.Contains(res.Message, "rerouted") {
		t.Fatalf("result must not claim reroute: %q", res.Message)
	}
}
