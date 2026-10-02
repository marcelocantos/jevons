// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleet"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/seatstop"
)

// 🎯T944: a burst of seats taken down by a planned broker restart raises no
// mass-stop alarm while they come back; the same burst unannounced (a crash)
// still alarms (🎯T925).
func TestT944PlannedBrokerRestartRaisesNoAlarm(t *testing.T) {
	burst := func(cause string) string {
		s := &Server{}
		reg := &fakeSweepReg{
			defs:      []claudia.AgentDef{{Name: "a", AutoStart: true}, {Name: "b", AutoStart: true}, {Name: "c", AutoStart: true}},
			hasProc:   map[string]bool{"a": true, "b": true, "c": true},
			alive:     map[string]bool{},
			causes:    map[string]string{"a": cause, "b": cause, "c": cause},
			launchErr: errBrokerDown,
		}
		s.noteDeadSeats(sweepDeadAgents(reg, "jevons", fleetintent.Snapshot{}))
		a, ok := s.massStop()
		if !ok {
			return ""
		}
		return a.Shared
	}
	if got := burst(fleet.ExitCauseBrokerRestarted); got != "" {
		t.Fatalf("a planned broker restart raised an alarm: %q", got)
	}
	if got := burst(fleet.ExitCauseBrokerLost); got == "" {
		t.Fatal("an unannounced broker loss raised no alarm")
	}
}

// 🎯T944: a planned stop whose seat has not come back once the grace has
// passed is an unintended consequence, and does alarm.
func TestT944PlannedStopStuckPastGraceAlarms(t *testing.T) {
	s := &Server{}
	past := time.Now().Add(-4 * time.Minute)
	for _, n := range []string{"a", "b", "c"} {
		s.seatStops().Note(recordAt(n, past))
	}
	if _, ok := s.massStop(); !ok {
		t.Fatal("seats still down past the planned grace raised no alarm")
	}
}

func recordAt(seat string, at time.Time) seatstop.Record {
	return seatstop.Record{Seat: seat, At: at, Source: seatstop.SourceBroker, Reason: seatstop.BrokerReason("", true), Planned: true}
}
