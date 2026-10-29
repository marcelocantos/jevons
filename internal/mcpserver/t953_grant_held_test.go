// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"errors"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleet"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

const grantHeldErr = `broker protocol: grant_held (name="jv-t953-worker"): grant jv-t953-worker is owned by another connection`

// 🎯T953: grant_held after a dead seat's relaunch leaves the row stopped and
// surfaces the refusal on the stop reason.
func TestT953GrantHeldRelaunchStoppedRow(t *testing.T) {
	t.Cleanup(func() { fleet.ClearGrantHeldRelaunch("jv-t953-worker") })
	s := &Server{}

	detail := "found not alive by the fleet feed; re-launch failed: " + grantHeldErr
	s.NoteDeadSeat("jv-t953-worker", "", detail)

	if s.seatAlive("jv-t953-worker") {
		t.Fatal("seatAlive must be false after grant_held relaunch failure")
	}
	reason, _, ok := s.SeatStopReason("jv-t953-worker")
	if !ok || !strings.Contains(reason, "grant_held") {
		t.Fatalf("stop reason = %q ok=%v, want grant_held detail", reason, ok)
	}

	// Broker still reporting the seat alive must not resurrect running.
	s.Seats().Observe(seatstate.Observation{
		Name: "jv-t953-worker", BrokerAlive: seatstate.Yes, BrokerOwned: seatstate.No,
		Source: "broker.seats", QueueDepth: seatstate.QueueUnknown,
	})
	if s.seatAlive("jv-t953-worker") {
		t.Fatal("broker-alive fold must not show running while grant_held pin is set")
	}
}

func TestT953GrantHeldRelaunchNotifiesParent(t *testing.T) {
	po := &fakeSender{alive: true}
	s, inbox := chainServer(t, map[string]*fakeSender{"jevons-po": po})
	detail := "found not alive by the fleet feed; re-launch failed: " + grantHeldErr
	s.noticeGrantHeldRelaunch("jevons-po", "jv-t953-worker", "t953-test", detail)
	if len(po.sent) != 1 {
		t.Fatalf("parent notices = %d, want 1: %v", len(po.sent), po.sent)
	}
	if !strings.Contains(po.sent[0], "jv-t953-worker") || !strings.Contains(po.sent[0], "grant_held") {
		t.Fatalf("parent notice: %s", po.sent[0])
	}
	if len(inbox.texts) != 1 {
		t.Fatalf("overseer fleet-health notices = %d", len(inbox.texts))
	}
}

// Sweep path: launch failure with grant_held is recorded the same way.
func TestT953SweepGrantHeldRelaunchFailsStopped(t *testing.T) {
	t.Cleanup(func() { fleet.ClearGrantHeldRelaunch("jv-t953-sweep") })
	s := &Server{}
	reg := &fakeSweepReg{
		defs:      []claudia.AgentDef{{Name: "jv-t953-sweep", AutoStart: true, Purpose: claudia.PurposeWork}},
		hasProc:   map[string]bool{"jv-t953-sweep": true},
		alive:     map[string]bool{"jv-t953-sweep": false},
		launchErr: errors.New(grantHeldErr),
	}
	s.noteDeadSeats(sweepDeadAgents(reg, "jevons", fleetintent.Snapshot{}, nil, nil))
	if s.seatAlive("jv-t953-sweep") {
		t.Fatal("want stopped row after sweep grant_held")
	}
	reason, _, ok := s.SeatStopReason("jv-t953-sweep")
	if !ok || !strings.Contains(reason, "grant_held") {
		t.Fatalf("reason=%q ok=%v", reason, ok)
	}
}
