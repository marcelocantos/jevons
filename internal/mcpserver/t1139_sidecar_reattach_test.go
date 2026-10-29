// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleet"
	"github.com/marcelocantos/jevons/internal/fleetintent"
)

const seatGoneCause = fleet.ExitCauseBrokerSeatGonePrefix + "process exited"

// 🎯T1139: a broker that stayed up but reported every sidecar seat gone
// must record broker stops, license reattach relaunch, and alarm once on a
// mass loss.
func TestT1139SeatGoneRecordsBrokerStopAndLicensesRelaunch(t *testing.T) {
	s := &Server{}
	reg := &fakeSweepReg{
		defs: []claudia.AgentDef{
			{Name: "w-sidecar", Purpose: claudia.PurposeWork},
		},
		hasProc: map[string]bool{"w-sidecar": true},
		alive:   map[string]bool{},
		causes:  map[string]string{"w-sidecar": seatGoneCause},
	}
	s.noteDeadSeats(sweepDeadAgents(reg, "jevons", fleetintent.Snapshot{}, nil, nil))

	reason, _, ok := s.SeatStopReason("w-sidecar")
	if !ok || !strings.HasPrefix(reason, "broker: ") {
		t.Fatalf("seat gone reason = %q ok=%v", reason, ok)
	}
	if !s.lostToBroker("w-sidecar") {
		t.Fatal("seat gone must license broker-lost reattach")
	}
}

func TestT1139SeatGoneExitCauseLicensesRelaunchBeforeLedger(t *testing.T) {
	s := &Server{}
	old := stoppedSeatCause
	stoppedSeatCause = func(_ *Server, name string) (string, bool) {
		if name == "w-live-ledger" {
			return seatGoneCause, true
		}
		return "", false
	}
	t.Cleanup(func() { stoppedSeatCause = old })
	if !s.lostToBroker("w-live-ledger") {
		t.Fatal("handle exit cause must license relaunch before seat_stop is written")
	}
}

func TestT1139MassSeatGoneNotifiesOnce(t *testing.T) {
	s := &Server{}
	reg := &fakeSweepReg{
		defs: []claudia.AgentDef{
			{Name: "a", AutoStart: true, Purpose: claudia.PurposeWork},
			{Name: "b", AutoStart: true, Purpose: claudia.PurposeWork},
			{Name: "c", AutoStart: true, Purpose: claudia.PurposeWork},
		},
		hasProc: map[string]bool{"a": true, "b": true, "c": true},
		alive:   map[string]bool{},
		causes:  map[string]string{"a": seatGoneCause, "b": seatGoneCause, "c": seatGoneCause},
	}
	s.noteDeadSeats(sweepDeadAgents(reg, "jevons", fleetintent.Snapshot{}, nil, nil))
	line1 := s.MassStopLine()
	line2 := s.MassStopLine()
	if line1 == "" {
		t.Fatal("mass sidecar loss raised no overseer line")
	}
	if line2 != line1 {
		t.Fatalf("mass loss notified twice: %q then %q", line1, line2)
	}
	if !strings.Contains(line1, "3") {
		t.Fatalf("mass line must name the count: %q", line1)
	}
}
