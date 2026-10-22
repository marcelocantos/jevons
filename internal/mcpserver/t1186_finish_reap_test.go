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
	"github.com/marcelocantos/jevons/internal/spool"
)

const t1186Worker = "jv-t1186-finish-reap"

func TestT1186FinishReportStoredInFlightReapsWhenTurnSettles(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: t1186Worker, WorkDir: dir, SessionID: "s1186",
		Purpose: claudia.PurposeWork, Parent: "jevons-po", TargetID: "T1186",
		Materialized: true, Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg, notifyJevon: func(string) {}}
	s.SetAgentReportDir(dir)

	finish := declaredFinishReport("T1186", "Done.")
	if envelope.ClassifyDone(finish) != envelope.DoneYes {
		t.Fatal("fixture must classify Done")
	}

	s.noteTurnInFlight(t1186Worker)
	s.storeAgentReport(t1186Worker, finish)
	if reg.Def(t1186Worker) == nil {
		t.Fatal("store must not reap while turn is in flight")
	}
	if suppress, reason := s.workerIdleSuppressReason(t1186Worker); !suppress || reason != "turn_in_flight" {
		t.Fatalf("worker-idle suppress=%v reason=%q want turn_in_flight", suppress, reason)
	}

	s.noteTurnEnded(t1186Worker)
	if reg.Def(t1186Worker) != nil {
		t.Fatal("seat must be reaped once the turn settles")
	}
}

func TestT1186IdleSweepReapsDoneReportWithoutNudge(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: t1186Worker, WorkDir: dir, SessionID: "s1186b",
		Purpose: claudia.PurposeWork, Parent: "jevons-po", TargetID: "T1186",
		Materialized: true, Provider: "grok", AutoStart: true,
	}); err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg, notifyJevon: func(string) {}}
	s.SetAgentReportDir(dir)
	finish := declaredFinishReport("T1186", "Done.")
	if _, err := agentreport.Save(dir, t1186Worker, finish, time.Now()); err != nil {
		t.Fatal(err)
	}

	var pushed []string
	activity := NewIdleActivityTracker()
	now := time.Unix(9000, 0)
	activity.by[t1186Worker] = IdleActivity{Phase: "idle", Updated: now.Add(-10 * time.Minute)}

	reps := SweepIdleNudges(IdleNudgeSweepArgs{
		Reg:                reg,
		Activity:           activity,
		Now:                now,
		LastTerminalReport: NewReportStoreLooksSatisfied(dir),
		TryReapStoredDone:  s.maybeReapStoredDoneWorkAgent,
		ProcessRunning:     func(string) bool { return true },
		Push: func(name, event, text string) error {
			pushed = append(pushed, text)
			return nil
		},
	})
	if reg.Def(t1186Worker) != nil {
		t.Fatal("idle sweep must reap a seat whose stored report classifies Done")
	}
	if len(pushed) != 0 {
		t.Fatalf("idle nudge delivered %d message(s): %v", len(pushed), pushed)
	}
	for _, r := range reps {
		if r.Name != t1186Worker {
			continue
		}
		if r.Action != IdleNudgeSkip || r.Reason != "reap_done" {
			t.Fatalf("classifier=%s/%s want skip/reap_done", r.Action, r.Reason)
		}
		return
	}
	t.Fatal("worker not evaluated in sweep")
}

func TestT1186EventSinkReapsFinishReportAfterInFlightIdle(t *testing.T) {
	s, reg := reapSinkServer(t, t1186Worker)
	dir := t.TempDir()
	s.SetAgentReportDir(dir)

	finish := declaredFinishReport("T1186", "Done.")
	sink := s.agentEventSink(t1186Worker)
	s.noteTurnInFlight(t1186Worker)
	if suppress, reason := s.workerIdleSuppressReason(t1186Worker); !suppress || reason != "turn_in_flight" {
		t.Fatalf("worker-idle suppress=%v reason=%q want turn_in_flight", suppress, reason)
	}

	sink(claudia.Event{Type: "assistant", Text: finish, StopReason: "end_turn"})
	if reg.Def(t1186Worker) != nil {
		t.Fatal("terminal finish-report must reap through agentEventSink")
	}
	if rec, err := agentreport.Latest(dir, t1186Worker); err != nil || !strings.Contains(rec.Text, "finish-report") {
		t.Fatalf("stored report missing after reap: err=%v", err)
	}
}

// TestT1186SidecarSpoolTurnEndReapsStoredFinishReport drives the production
// seat-observation path: a sidecar turn_end in the dated spool settles flight
// so a stored done finish-report reaps (🎯T1186 / T419 call-site reachability).
func TestT1186SidecarSpoolTurnEndReapsStoredFinishReport(t *testing.T) {
	spoolDir := t.TempDir()
	t.Setenv(spool.DirEnv, spoolDir)
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	const worker = "jv-t1186-sidecar-finish"
	def := claudia.AgentDef{
		Name: worker, WorkDir: dir, SessionID: "s1186-sidecar",
		Purpose: claudia.PurposeWork, Parent: "jevons-po", TargetID: "T1186",
		Provider: "grok", Materialized: true,
	}
	if err := reg.Register(def); err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.SetAgentReportDir(dir)
	s.SetSeatAliveFn(func(name string) bool { return name == worker })

	finish := declaredFinishReport("T1186", "Done.")
	s.noteTurnInFlight(worker)
	s.storeAgentReport(worker, finish)
	if reg.Def(worker) == nil {
		t.Fatal("store must not reap while flight is in flight")
	}
	if suppress, reason := s.workerIdleSuppressReason(worker); !suppress || reason != "turn_in_flight" {
		t.Fatalf("worker-idle suppress=%v reason=%q want turn_in_flight", suppress, reason)
	}

	plantT1080SpoolLine(t, spoolDir, worker, time.Now().UTC(), "turn_end")
	s.observeRegistryLiveness()
	s.observeSeatTranscriptWith(def, s.transcriptRoots())

	if s.flightState(worker) == FlightInFlight {
		t.Fatal("spool turn_end must settle flight for a sidecar seat")
	}
	if reg.Def(worker) != nil {
		t.Fatal("stored done finish-report must reap once spool turn_end settles flight")
	}
}
