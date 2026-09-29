// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"errors"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleetintent"
)

var errBrokerDown = errors.New("claudia: broker connection closed")

// 🎯T925: a seat whose broker connection closed records the broker as its
// stop, on the sweep and on the fleet feed's path alike, and only such a
// seat is one the reattach loop may relaunch. A death nothing explained
// stays "unknown".
func TestT925ABrokerStopIsRecordedAsTheBrokers(t *testing.T) {
	s := &Server{}
	reg := &fakeSweepReg{
		defs: []claudia.AgentDef{
			{Name: "w-broker", AutoStart: true, Purpose: claudia.PurposeWork},
			{Name: "w-plain", AutoStart: true, Purpose: claudia.PurposeWork},
		},
		hasProc:   map[string]bool{"w-broker": true, "w-plain": true},
		alive:     map[string]bool{},
		causes:    map[string]string{"w-broker": claudia.ExitCauseBrokerLost},
		launchErr: errBrokerDown,
	}
	reps := sweepDeadAgents(reg, "jevons", fleetintent.Snapshot{})
	s.noteDeadSeats(reps)

	reason, _, ok := s.SeatStopReason("w-broker")
	if !ok || !strings.HasPrefix(reason, "broker: claudia broker connection closed") {
		t.Fatalf("broker seat reason = %q ok=%v", reason, ok)
	}
	if !s.lostToBroker("w-broker") {
		t.Fatal("a seat the broker took down is not marked lost to it")
	}
	if reason, _, _ := s.SeatStopReason("w-plain"); !strings.HasPrefix(reason, "unknown") {
		t.Fatalf("unexplained death reason = %q", reason)
	}
	if s.lostToBroker("w-plain") {
		t.Fatal("an unexplained death licenses a relaunch")
	}

	// The fleet feed's own record of a dead seat.
	s.NoteDeadSeat("w-feed", claudia.ExitCauseBrokerLost, "found not alive by the fleet feed; re-launch failed")
	if reason, _, _ := s.SeatStopReason("w-feed"); !strings.HasPrefix(reason, "broker: ") {
		t.Fatalf("feed-noted reason = %q", reason)
	}

	// A deliberate stop afterwards replaces the broker record.
	s.noteSeatStop("w-broker", "supervisor", "jevons_agent_stop by owner", "owner", "")
	if s.lostToBroker("w-broker") {
		t.Fatal("a deliberate stop still reads as lost to the broker")
	}
}
