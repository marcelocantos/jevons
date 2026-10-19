// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/agentreport"
	"github.com/marcelocantos/jevons/internal/envelope"
)

const t757Worker = "jv-t757-probe"

func TestT757FiveHourStoredReportCarriesAge(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	storedAt := now.Add(-5 * time.Hour)
	h := agentreport.Handle{
		Agent:    t757Worker,
		ReportID: "20260921T070000Z-abcd",
		StoredAt: storedAt,
	}
	line := agentRespondedRoutingLine(t757Worker, h, now)
	if !strings.Contains(line, "report_id=") {
		t.Fatalf("routing line missing report_id: %q", line)
	}
	if !strings.Contains(line, "age=5h") {
		t.Fatalf("routing line missing ~5h age: %q", line)
	}
}

func TestT757MutationOmittingAgeGoesRed(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	h := agentreport.Handle{
		Agent:    t757Worker,
		ReportID: "20260921T110000Z-ef01",
		StoredAt: now.Add(-2 * time.Hour),
	}
	line := agentRespondedRoutingLine(t757Worker, h, now)
	if !strings.Contains(line, " age=") {
		t.Fatal("mutation: routing line with stored report must carry age=")
	}
}

func TestT757QueuedDeliveryStampsAgeAtFlush(t *testing.T) {
	s, parent, _, _ := t731Server(t)
	now := time.Date(2026, 9, 21, 18, 0, 0, 0, time.UTC)
	storedAt := now.Add(-5 * time.Hour)
	s.reportDeliveryNow = func() time.Time { return now }

	rec, err := agentreport.Save(s.agentReportStateDir(), t731Worker, t731Report, storedAt)
	if err != nil {
		t.Fatal(err)
	}
	// Queued copy may have been formatted before flush without a fresh age.
	queued := envelope.SeatFact(envelope.IntentRoute, t731Worker) + " report_id=" + rec.ID + "\n" + t731Report

	s.noteTurnInFlight(t731Parent)
	res, err := s.deliverByName(t731Parent, queued, OriginAgent, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "queued" && !strings.Contains(res.Status, "queue") {
		t.Fatalf("expected queued delivery, got status=%q msg=%q", res.Status, res.Message)
	}
	s.setFlight(t731Parent, FlightIdle)
	s.drainAgentSendQueue(t731Parent)
	if len(parent.sent) != 1 {
		t.Fatalf("parent deliveries=%d want 1; %v", len(parent.sent), parent.sent)
	}
	if !strings.Contains(parent.sent[0], "age=5h") {
		t.Fatalf("flushed parent copy missing ~5h age:\n%s", parent.sent[0])
	}
}

func TestT757LiveParentDeliveryIncludesAge(t *testing.T) {
	s, parent, _, _ := t731Server(t)
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	s.reportDeliveryNow = func() time.Time { return now }

	s.notify(t731Live, t731Report)
	if len(parent.sent) != 1 {
		t.Fatalf("parent deliveries=%d want 1; %v", len(parent.sent), parent.sent)
	}
	lines := strings.Split(parent.sent[0], "\n")
	if len(lines) < 2 || lines[0] != envelope.PrefixAgentResponded {
		t.Fatalf("parent copy missing the agent-responded fact:\n%s", parent.sent[0])
	}
	if !strings.Contains(lines[1], "report_id=") {
		t.Fatalf("routing line missing report_id: %q", lines[1])
	}
	if !strings.Contains(lines[1], " age=") {
		t.Fatalf("routing line missing age: %q", lines[1])
	}
}

func TestParseAgentRespondedWithAge(t *testing.T) {
	t.Parallel()
	name, id, ok := parseAgentRespondedLine(envelope.SeatFact(envelope.IntentRoute, t757Worker) + " report_id=20260921T010203Z-abcd age=5h")
	if !ok || name != t757Worker || id != "20260921T010203Z-abcd" {
		t.Fatalf("with age: name=%q id=%q ok=%v", name, id, ok)
	}
}
