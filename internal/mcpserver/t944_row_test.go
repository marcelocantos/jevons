// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/seatstop"
)

// 🎯T944: a fleet row stopped by a planned broker restart paints no reason
// while its seat comes back — the owner called the "planned Claudia broker
// restart" line of no value — and says it did not come back once the grace
// has passed. An unplanned stop still shows its reason.
func TestT944PlannedStopRowIsQuietUntilTheGracePasses(t *testing.T) {
	s := &Server{}
	s.seatStops().Note(recordAt("recovering", time.Now()))
	s.seatStops().Note(recordAt("stuck", time.Now().Add(-seatstop.PlannedGrace-time.Minute)))
	s.seatStops().Note(seatstop.Record{Seat: "crashed", At: time.Now(), Source: seatstop.SourceExit, Reason: "exit status 2"})

	if reason, _, ok := s.SeatStopShown("recovering"); !ok || reason != "" {
		t.Fatalf("recovering row = %q (ok=%v), want no reason", reason, ok)
	}
	if reason, _, _ := s.SeatStopShown("stuck"); reason != PlannedNotBack {
		t.Fatalf("stuck row = %q, want %q", reason, PlannedNotBack)
	}
	if reason, _, _ := s.SeatStopShown("crashed"); reason != "exit status 2" {
		t.Fatalf("unplanned row = %q", reason)
	}
	// The record itself keeps the full reason for agent_list and rehydrate.
	if reason, _, _ := s.SeatStopReason("recovering"); reason == "" {
		t.Fatal("the planned stop's record lost its reason")
	}
}

// 🎯T944: an announced broker restart is recorded even when an unplanned
// stop was noted for the seat moments before; the two-minute dedupe used to
// keep the older record, so the row kept saying the seat had died.
func TestT944AnnouncedRestartReplacesARecentUnplannedStop(t *testing.T) {
	s := &Server{}
	s.seatStops().Note(seatstop.Record{Seat: "a", At: time.Now(), Source: seatstop.SourceExit, Reason: "agent lifecycle operation in progress"})
	reg := &fakeSweepReg{
		defs:      []claudia.AgentDef{{Name: "a", AutoStart: true}},
		hasProc:   map[string]bool{"a": true},
		alive:     map[string]bool{},
		causes:    map[string]string{"a": claudia.ExitCauseBrokerRestarted},
		launchErr: errBrokerDown,
	}
	s.noteDeadSeats(sweepDeadAgents(reg, "jevons", fleetintent.Snapshot{}))
	last, ok := s.seatStops().Last("a")
	if !ok || !last.Planned {
		t.Fatalf("last record %+v, want the announced planned restart", last)
	}
	if reason, _, _ := s.SeatStopShown("a"); reason != "" {
		t.Fatalf("row shows %q while the seat comes back", reason)
	}

	// A second announced stop within the window does not churn the record.
	at := last.At
	s.noteDeadSeats(sweepDeadAgents(reg, "jevons", fleetintent.Snapshot{}))
	if again, _ := s.seatStops().Last("a"); !again.At.Equal(at) {
		t.Fatal("a repeated planned stop re-recorded within the dedupe window")
	}
}

// 🎯T983: a seat its PO parked, then stopped by a planned broker restart, is
// not one that failed to come back: the row names the park. On 2026-10-02
// three finished or blocked workers read "did not come back after a planned
// broker restart", and the owner took them for failing restarts.
func TestT983ParkedSeatRowNamesTheParkNotTheRestart(t *testing.T) {
	s := &Server{}
	st, err := fleetintent.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.SetFleetIntentStore(st)
	if err := s.SetAgentIntent("done", fleetintent.Parked, "jevons-po", "T972 fully achieved"); err != nil {
		t.Fatal(err)
	}
	s.seatStops().Note(recordAt("done", time.Now().Add(-seatstop.PlannedGrace-time.Minute)))
	s.seatStops().Note(recordAt("open", time.Now().Add(-seatstop.PlannedGrace-time.Minute)))

	reason, _, _ := s.SeatStopShown("done")
	if want := "parked by owner/overseer (jevons-po): T972 fully achieved"; reason != want {
		t.Fatalf("parked row = %q, want %q", reason, want)
	}
	// A seat whose intent is still working did fail to come back.
	if reason, _, _ := s.SeatStopShown("open"); reason != PlannedNotBack {
		t.Fatalf("working row = %q, want %q", reason, PlannedNotBack)
	}
}
