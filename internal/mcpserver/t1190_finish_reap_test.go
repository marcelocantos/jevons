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
)

const t1190Worker = "jv-t1190-reverify"

const t1190ProseReverify = "Re-verifying state after the host restart. Work is complete; oracle was cited in the prior turn."

// TestT1190ProseTurnReapsFromStoredFinishReport drives maybeReapDoneWorkAgent
// (🎯T419 production caller) when the turn text declares nothing but an earlier
// stored finish-report classifies Done (🎯T1190 design B).
func TestT1190ProseTurnReapsFromStoredFinishReport(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: t1190Worker, WorkDir: dir, SessionID: "s1190",
		Purpose: claudia.PurposeWork, Parent: "jevons-po", TargetID: "T1190",
		Materialized: true, Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg, notifyJevon: func(string) {}}
	s.SetAgentReportDir(dir)

	finish := declaredFinishReport("T1190", "Done.")
	if _, err := agentreport.Save(dir, t1190Worker, finish, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := agentreport.Save(dir, t1190Worker, t1190ProseReverify, time.Now()); err != nil {
		t.Fatal(err)
	}

	s.maybeReapDoneWorkAgent(t1190Worker, t1190ProseReverify)
	if reg.Def(t1190Worker) != nil {
		t.Fatal("prose re-verification must not hide a stored done finish-report from reap")
	}
}

// TestT1190StoredDoneReapsDespiteStaleInFlightLatch: sidecar seats can keep a
// turn_in_flight latch while phase is idle; stored done finish still reaps.
func TestT1190StoredDoneReapsDespiteStaleInFlightLatch(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: t1190Worker, WorkDir: dir, SessionID: "s1190b",
		Purpose: claudia.PurposeWork, Parent: "jevons-po", TargetID: "T1190",
		Materialized: true, Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg, notifyJevon: func(string) {}}
	s.SetAgentReportDir(dir)
	s.noteTurnInFlight(t1190Worker)

	finish := declaredFinishReport("T1190", "Done.")
	if _, err := agentreport.Save(dir, t1190Worker, finish, time.Now()); err != nil {
		t.Fatal(err)
	}
	if s.flightState(t1190Worker) != FlightInFlight {
		t.Fatal("fixture must read in flight")
	}
	if !s.maybeReapStoredDoneWorkAgent(t1190Worker) {
		t.Fatal("stored done finish-report must reap despite stale in-flight latch")
	}
	if reg.Def(t1190Worker) != nil {
		t.Fatal("seat must leave registry after stored-done reap")
	}
}

// TestT1190EventSinkProseAfterStoredFinish drives agentEventSink end-to-end.
func TestT1190EventSinkProseAfterStoredFinish(t *testing.T) {
	s, reg := reapSinkServer(t, t1190Worker)
	dir := t.TempDir()
	s.SetAgentReportDir(dir)

	finish := declaredFinishReport("T1190", "Done.")
	if _, err := agentreport.Save(dir, t1190Worker, finish, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if envelope.ClassifyDone(finish) != envelope.DoneYes {
		t.Fatal("fixture finish must classify Done")
	}

	sink := s.agentEventSink(t1190Worker)
	sink(claudia.Event{Type: "assistant", Text: t1190ProseReverify, StopReason: "end_turn"})
	if reg.Def(t1190Worker) != nil {
		t.Fatal("terminal prose turn must reap via stored declared finish-report")
	}
}

func t1190PlacementBadFinish() string {
	fence := "```jevons\n" +
		"jevons: kind finish-report\n" +
		"jevons: target T1190\n" +
		"jevons: oracle sha=2ca7a7d4186c5d83b5fac4c467d1da9c192892b7\n" +
		"jevons: silent-ledger none\n" +
		"```\n"
	return "Summary first.\n\n" + fence + "Post-fence prose the parser rejects.\n"
}

// TestT1190PlacementCorrectionNotifiesAuthorOnce drives agentEventSink when a
// trailing finish-report is followed by non-GOAL_STATUS prose.
func TestT1190PlacementCorrectionNotifiesAuthorOnce(t *testing.T) {
	self := &fakeSender{alive: true}
	parent := &fakeSender{alive: true}
	const t1190Parent = "jevons-po"
	worker := "jv-t1190-placement"
	s, _ := chainServer(t, map[string]*fakeSender{
		worker:      self,
		t1190Parent: parent,
	})
	s.registry = newLineageRegistry(t, map[string]string{
		t1190Parent: "jevons",
		worker:      t1190Parent,
	})
	s.SetAgentReportDir(t.TempDir())

	raw := t1190PlacementBadFinish()
	if _, ok := envelope.PlacementRejectReason(raw); !ok {
		t.Fatal("fixture must be a placement reject")
	}

	sink := s.agentEventSink(worker)
	sink(claudia.Event{Type: "assistant", Text: raw, StopReason: "end_turn"})
	if len(self.sent) != 1 {
		t.Fatalf("author must receive one placement correction, got %d", len(self.sent))
	}
	if !strings.Contains(self.sent[0], "placement_prose_after_fence") {
		t.Fatalf("correction missing reason: %q", self.sent[0])
	}
	if s.registry.Def(worker) == nil {
		t.Fatal("placement reject must not reap the author before resend")
	}

	sink(claudia.Event{Type: "assistant", Text: raw, StopReason: "end_turn"})
	if len(self.sent) != 1 {
		t.Fatalf("duplicate terminal turn must not re-notify, got %d messages", len(self.sent))
	}
}

// TestT1190PlacementCorrectedLine1ResendReaps uses maybeReapDoneWorkAgent after
// a valid line-1 finish-report.
func TestT1190PlacementCorrectedLine1ResendReaps(t *testing.T) {
	s, reg := reapSinkServer(t, "jv-t1190-line1")
	s.SetAgentReportDir(t.TempDir())

	corrected := declaredFinishReport("T1190", "Done after placement fix.")
	s.maybeReapDoneWorkAgent("jv-t1190-line1", corrected)
	if reg.Def("jv-t1190-line1") != nil {
		t.Fatal("valid line-1 finish-report must reap")
	}
}
