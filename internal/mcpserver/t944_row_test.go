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
