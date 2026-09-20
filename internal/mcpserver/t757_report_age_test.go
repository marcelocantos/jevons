// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/agentreport"
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
	queued := "[Agent " + t731Worker + " responded] report_id=" + rec.ID + "\n" + t731Report

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
	firstLine := strings.SplitN(parent.sent[0], "\n", 2)[0]
	if !strings.Contains(firstLine, "report_id=") {
		t.Fatalf("routing line missing report_id: %q", firstLine)
	}
	if !strings.Contains(firstLine, " age=") {
		t.Fatalf("routing line missing age: %q", firstLine)
	}
}

func TestParseAgentRespondedWithAge(t *testing.T) {
	t.Parallel()
	name, id, ok := parseAgentRespondedLine("[Agent jv-t757-probe responded] report_id=20260921T010203Z-abcd age=5h")
	if !ok || name != t757Worker || id != "20260921T010203Z-abcd" {
		t.Fatalf("with age: name=%q id=%q ok=%v", name, id, ok)
	}
}
