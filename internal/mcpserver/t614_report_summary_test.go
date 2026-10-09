// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/roles"
)

// t614FinishReport is the 2026-09-01 jv-t611-month-trim oracle-backed
// finish-report. First send to jevons-po injects the standing brief and
// product-owner doctrine; the skipped-hop record must still name the work
// (🎯T614).
const t614FinishReport = "```jevons\n" +
	"jevons: kind finish-report\n" +
	"jevons: target T611\n" +
	"jevons: oracle sha=b695174d gate-id=8544c7b4\n" +
	"jevons: verdict GREEN\n" +
	"jevons: silent-ledger none\n" +
	"```\n\n" +
	"Month-trim landed."

func TestT614FinishReportRecordCitesOracleNotDoctrine(t *testing.T) {
	s, po, inbox, logPath := t658Server(t)
	s.registry = newLineageRegistry(t, map[string]string{
		"jevons-po":          "jevons",
		"jv-t611-month-trim": "jevons-po",
	})

	res := t658Send(t, s, "jv-t611-month-trim", t614FinishReport)
	got := toolText(res)
	if !strings.Contains(got, "standing fleet brief") {
		t.Fatalf("fixture must be a first send (brief injected): %s", got)
	}
	if !strings.Contains(got, "rerouted") {
		t.Fatalf("oracle finish-report must skip the PO hop: %s", got)
	}
	if len(inbox.texts) != 1 || !strings.Contains(inbox.texts[0], "Month-trim landed") {
		t.Fatalf("overseer inbox=%v want the full finish-report", inbox.texts)
	}
	if len(po.sent) != 1 || !strings.Contains(po.sent[0], "routed to overseer") {
		t.Fatalf("PO inbox=%v want one record line", po.sent)
	}
	record := po.sent[0]
	if !strings.Contains(record, "T611") {
		t.Errorf("PO record missing target id T611: %q", record)
	}
	if !strings.Contains(record, "b695174d") && !strings.Contains(record, "8544c7b4") {
		t.Errorf("PO record missing SHA or GATE: %q", record)
	}
	if strings.Contains(record, "Product-owner role") || strings.Contains(record, roles.DoctrineMarker) {
		t.Errorf("PO record summarized the doctrine: %q", record)
	}
	if strings.Contains(record, "[Jevons fleet standing brief") {
		t.Errorf("PO record summarized the standing brief: %q", record)
	}
	if ev := t658RelayEvents(t, logPath, "jv-t611-month-trim"); len(ev) != 1 {
		t.Fatalf("relayroute events=%d want 1: %v", len(ev), ev)
	}
}
