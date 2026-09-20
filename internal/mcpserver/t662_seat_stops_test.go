// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/seatstop"
)

// 🎯T662 acceptance 1: a stop the daemon performs or discovers is recorded on
// the seat and journalled as one agent_lifecycle.seat_stop line.
func TestT662StopIsRecordedAndJournalled(t *testing.T) {
	s := &Server{}
	var journal []map[string]any
	s.eventLogger = func(component, decision string, fields map[string]any) {
		if component == compAgentLifecycle && decision == "seat_stop" {
			journal = append(journal, fields)
		}
	}
	s.noteSeatStop("jv-t662-a", seatstop.SourceSupervisor, "jevons_agent_stop by jevons: parked for the night", "jevons", "")
	s.noteDeadSeats([]DeadAgentReport{{Name: "jv-t662-b", Removed: true}})

	if len(journal) != 2 {
		t.Fatalf("journal lines = %d, want 2", len(journal))
	}
	if journal[0]["name"] != "jv-t662-a" || journal[0]["reason"] != "jevons_agent_stop by jevons: parked for the night" {
		t.Fatalf("first line = %+v", journal[0])
	}
	if journal[1]["name"] != "jv-t662-b" || journal[1]["reason"] != seatstop.Unknown || journal[1]["source"] != string(seatstop.SourceExit) {
		t.Fatalf("discovered death must be recorded as unknown, never silent: %+v", journal[1])
	}
	if reason, _, ok := s.SeatStopReason("jv-t662-b"); !ok || reason != seatstop.Unknown {
		t.Fatalf("SeatStopReason = %q %v", reason, ok)
	}
	// A seat the daemon just stopped on purpose keeps that reason through
	// the next sweep rather than being overwritten with unknown.
	s.noteDeadSeats([]DeadAgentReport{{Name: "jv-t662-a"}})
	if reason, _, _ := s.SeatStopReason("jv-t662-a"); !strings.HasPrefix(reason, "jevons_agent_stop") {
		t.Fatalf("sweep overwrote a recorded reason: %q", reason)
	}
}

// 🎯T662 acceptance 2: three seats within a minute, no restart in the
// window → one alert leading agent_list, naming the shared reason, keyed
// once per burst for the overseer note.
func TestT662MassStopAlertOnAgentListAndOnce(t *testing.T) {
	s := &Server{}
	s.mu.Lock()
	s.bootAt = time.Now().Add(-time.Hour)
	s.mu.Unlock()

	for _, n := range []string{"jv-t657-steer-ui", "jv-t658-hop-classifier", "jv-t659-clean-web-gate"} {
		s.noteDeadSeats([]DeadAgentReport{{Name: n}})
	}
	body := s.withMassStop("jevons-po   idle ...")
	if !strings.HasPrefix(body, "MASS STOP") || !strings.Contains(body, "unknown: 3 seats, no reason recorded") {
		t.Fatalf("agent_list did not lead with the alert:\n%s", body)
	}
	for _, n := range []string{"jv-t657-steer-ui", "jv-t658-hop-classifier", "jv-t659-clean-web-gate"} {
		if !strings.Contains(body, n) {
			t.Fatalf("alert does not name %s:\n%s", n, body)
		}
	}
	s.mu.Lock()
	key := s.massStopNotified
	s.mu.Unlock()
	if key == "" {
		t.Fatal("burst was not marked as delivered to the overseer")
	}
	s.withMassStop("again")
	s.mu.Lock()
	again := s.massStopNotified
	s.mu.Unlock()
	if again != key {
		t.Fatalf("second read re-keyed the same burst: %q → %q", key, again)
	}
}

// A restart inside the burst is the restart path's story: no mass-stop alert.
func TestT662RestartInsideBurstIsSilentHere(t *testing.T) {
	s := &Server{}
	for _, n := range []string{"a", "b", "c"} {
		s.noteDeadSeats([]DeadAgentReport{{Name: n}})
	}
	s.mu.Lock()
	s.bootAt = time.Now()
	s.mu.Unlock()
	if line := s.MassStopLine(); line != "" {
		t.Fatalf("burst around a daemon boot reported as a mass stop: %s", line)
	}
}

// 🎯T662 acceptance 3: the rehydrate suffix names the recorded reason.
func TestT662RehydratedAfterNamesTheReason(t *testing.T) {
	s := &Server{}
	if got := s.rehydratedAfter("jv-none"); got != " (rehydrated after dead/stopped process)" {
		t.Fatalf("no record: %q", got)
	}
	s.noteSeatStop("jv-t662-c", seatstop.SourcePlanPolicy, "plan policy parked: claude weekly exhausted", "jevons", "")
	if got := s.rehydratedAfter("jv-t662-c"); got != " (rehydrated after plan policy parked: claude weekly exhausted)" {
		t.Fatalf("recorded: %q", got)
	}
}
