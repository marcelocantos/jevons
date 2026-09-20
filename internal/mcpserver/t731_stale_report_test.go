// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/agentreport"
	"github.com/marcelocantos/jevons/internal/envelope"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/fleetlog"
)

const (
	t731Worker = "jv-t731-probe"
	t731Parent = "jevons-po"
	t731Live   = "jv-t731-live"
	t731Report = "Ledger still identified — achieve is the PO's. SHA abcdef0123456."
)

func t731Server(t *testing.T) (*Server, *fakeSender, *fakeSender, *overseerInbox) {
	t.Helper()
	parent := &fakeSender{alive: true}
	live := &fakeSender{alive: true}
	s, inbox := chainServer(t, map[string]*fakeSender{
		t731Parent: parent,
		t731Live:   live,
	})
	dir := t.TempDir()
	s.stateDir = dir
	s.SetAgentReportDir(dir)
	s.SetSendQueueDir(dir)
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{
		{Name: t731Parent, WorkDir: dir, SessionID: "s-po", Purpose: claudia.PurposeWork, Parent: "jevons"},
		{Name: t731Worker, WorkDir: dir, SessionID: "s-w", Purpose: claudia.PurposeWork, Parent: t731Parent, TargetID: "T731"},
		{Name: t731Live, WorkDir: dir, SessionID: "s-live", Purpose: claudia.PurposeWork, Parent: t731Parent, TargetID: "T731"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	s.registry = reg
	store, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.SetFleetIntentStore(store)
	s.SetRemovalAccount(fleetlog.New(nil))
	s.RemovalAccount().SetRemovedHook(func(name string, rm fleetlog.Removal) {
		by := "product:" + rm.Reason
		s.MarkAgentReaped(name, by, rm.Detail)
	})
	return s, parent, live, inbox
}

func t731Reap(t *testing.T, s *Server, name string) {
	t.Helper()
	ok, err := s.RemovalAccount().Remove(s.registry, name, fleetlog.Removal{
		Reason: fleetlog.ReasonReapDone,
		Detail: "reaped on a finished-work report (finished_work)",
	})
	if err != nil || !ok {
		t.Fatalf("reap %s: ok=%v err=%v", name, ok, err)
	}
	if _, reaped := LookupReapedRecord(s.fleetIntent(), name); !reaped {
		t.Fatal("reaped intent missing")
	}
}

func TestParseAgentResponded(t *testing.T) {
	t.Parallel()
	name, id, ok := parseAgentRespondedLine("[Agent jv-t731-probe responded]")
	if !ok || name != t731Worker || id != "" {
		t.Fatalf("plain: name=%q id=%q ok=%v", name, id, ok)
	}
	name, id, ok = parseAgentRespondedLine("[Agent jv-t731-probe responded] report_id=20260921T010203Z-abcd")
	if !ok || name != t731Worker || id != "20260921T010203Z-abcd" {
		t.Fatalf("with id: name=%q id=%q ok=%v", name, id, ok)
	}
	if _, _, ok := parseAgentRespondedLine("[held backlog from jv-x]"); ok {
		t.Fatal("non-report parsed as agent-responded")
	}
}

func TestFormatReapedReportBannerCarriesTime(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 9, 20, 15, 42, 7, 0, time.UTC)
	got := FormatReapedReportBanner("jv-t723-empty-report", fleetintent.Record{At: at})
	if !strings.Contains(got, "2026-09-20T15:42:07Z") {
		t.Fatalf("missing reap time: %q", got)
	}
	if !strings.Contains(got, "not current ledger or fleet state") {
		t.Fatalf("missing stale-claim sentence: %q", got)
	}
	if !strings.HasPrefix(got, reapedSeatPrefix) {
		t.Fatalf("prefix: %q", got)
	}
}

func TestT731QueuedReapedReportIsMarkedOnce(t *testing.T) {
	s, parent, _, _ := t731Server(t)
	s.noteTurnInFlight(t731Parent)

	s.notify(t731Worker, t731Report)
	if len(parent.sent) != 0 {
		t.Fatalf("parent received while in flight: %v", parent.sent)
	}
	if s.pendingAgentSends(t731Parent) != 1 {
		t.Fatalf("queued depth=%d want 1", s.pendingAgentSends(t731Parent))
	}
	recs, err := agentreport.List(s.agentReportStateDir(), t731Worker)
	if err != nil || len(recs) != 1 {
		t.Fatalf("stored reports=%d err=%v", len(recs), err)
	}
	reportID := recs[0].ID

	t731Reap(t, s, t731Worker)

	s.setFlight(t731Parent, FlightIdle)
	s.drainAgentSendQueue(t731Parent)
	if len(parent.sent) != 1 {
		t.Fatalf("parent deliveries=%d want 1 (queued copy flushed once); got %v", len(parent.sent), parent.sent)
	}
	got := parent.sent[0]
	if !strings.Contains(got, reapedSeatPrefix) {
		t.Fatalf("queued post-reap copy was unmarked:\n%s", got)
	}
	rec, _ := LookupReapedRecord(s.fleetIntent(), t731Worker)
	wantAt := rec.At.UTC().Format(time.RFC3339)
	if !strings.Contains(got, wantAt) {
		t.Fatalf("marker missing reap time %s:\n%s", wantAt, got)
	}
	if !strings.Contains(got, t731Report) {
		t.Fatalf("suppressed the report body (🎯T690):\n%s", got)
	}
	if !strings.Contains(got, "report_id="+reportID) {
		t.Fatalf("parent copy missing report id %s:\n%s", reportID, got)
	}

	res, err := s.deliverByName(t731Parent, got, OriginAgent, false)
	if err != nil {
		t.Fatalf("second offer: %v", err)
	}
	if res.Status != StatusSuppressedDuplicateReport {
		t.Fatalf("second offer status=%q want %s: %s", res.Status, StatusSuppressedDuplicateReport, res.Message)
	}
	if len(parent.sent) != 1 {
		t.Fatalf("second offer leaked onto parent: %v", parent.sent)
	}
}

func TestT731LiveSeatReportIsUnmarked(t *testing.T) {
	s, parent, live, inbox := t731Server(t)
	s.notify(t731Live, t731Report)
	if len(parent.sent) != 1 {
		t.Fatalf("parent deliveries=%d want 1; %v", len(parent.sent), parent.sent)
	}
	if strings.Contains(parent.sent[0], reapedSeatPrefix) {
		t.Fatalf("live seat marked as reaped:\n%s", parent.sent[0])
	}
	if !strings.Contains(parent.sent[0], "[Agent "+t731Live+" responded") {
		t.Fatalf("live parent copy lost framing:\n%s", parent.sent[0])
	}
	if len(inbox.texts) != 1 {
		t.Fatalf("overseer deliveries=%d want 1", len(inbox.texts))
	}
	if strings.Contains(inbox.texts[0], reapedSeatPrefix) {
		t.Fatalf("overseer copy of a live report was marked:\n%s", inbox.texts[0])
	}
	if inbox.texts[0] != "[Agent "+t731Live+" responded]\n"+t731Report {
		t.Fatalf("overseer short-report chrome drifted:\n%q", inbox.texts[0])
	}
	_ = live
}

func TestT731DroppingTheFirstCopyGoesRed(t *testing.T) {
	s, parent, _, _ := t731Server(t)
	s.noteTurnInFlight(t731Parent)
	s.notify(t731Worker, t731Report)
	t731Reap(t, s, t731Worker)
	s.setFlight(t731Parent, FlightIdle)
	s.drainAgentSendQueue(t731Parent)
	if len(parent.sent) == 0 {
		t.Fatal("mutation: first parent copy was dropped — 🎯T690")
	}
}

func TestT731ReapedBannerStillParsesEnvelope(t *testing.T) {
	t.Parallel()
	inner := envelope.Format(&envelope.Message{
		Kind:         envelope.KindFinishReport,
		Target:       "T731",
		SHA:          "abcdef0123456",
		SilentLedger: envelope.SilentLedgerEmpty,
		Payload:      t731Report,
	})
	wrapped := FormatReapedReportBanner(t731Worker, fleetintent.Record{
		At: time.Date(2026, 9, 20, 15, 42, 7, 0, time.UTC),
	}) + "\n[Agent " + t731Worker + " responded] report_id=abc\n" + inner
	got, err := envelope.Parse(wrapped)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got == nil || got.Kind != envelope.KindFinishReport || got.Target != "T731" {
		t.Fatalf("got %+v", got)
	}
}

// 🎯T747: the same report body stored twice has two ids (timestamped), so the
// id-keyed ledger alone offers it twice. Route twice, assert one delivery.
func TestT747IdenticalBodyUnderNewIDIsNotReDelivered(t *testing.T) {
	s, parent, _, _ := t731Server(t)

	s.notify(t731Worker, t731Report)
	if len(parent.sent) != 1 {
		t.Fatalf("first delivery: parent got %d want 1: %v", len(parent.sent), parent.sent)
	}
	// Same body, later second: a distinct stored id.
	time.Sleep(1100 * time.Millisecond)
	s.notify(t731Worker, t731Report)

	recs, err := agentreport.List(s.agentReportStateDir(), t731Worker)
	if err != nil || len(recs) != 2 || recs[0].ID == recs[1].ID {
		t.Fatalf("want two stored reports with distinct ids; got %d err=%v", len(recs), err)
	}
	if len(parent.sent) != 1 {
		t.Fatalf("identical body re-delivered to the parent that already consumed it: %d deliveries: %v",
			len(parent.sent), parent.sent)
	}
}

// Control: a different body from the same agent still reaches the parent.
func TestT747DifferentBodyStillDelivered(t *testing.T) {
	s, parent, _, _ := t731Server(t)
	s.notify(t731Worker, t731Report)
	s.notify(t731Worker, t731Report+" And a second, different finding.")
	if len(parent.sent) != 2 {
		t.Fatalf("parent deliveries=%d want 2: %v", len(parent.sent), parent.sent)
	}
}
