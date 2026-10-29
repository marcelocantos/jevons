// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/seatstop"
	"github.com/marcelocantos/jevons/internal/sendq"
)

func TestT1263PreStopPolicyHeldQueueIsNotStandingAlarm(t *testing.T) {
	s, _ := t530Server(t)
	name := "jv-t1111-auto"
	before := time.Now().Add(-171 * time.Hour).UTC()
	if _, _, err := s.sendQueue().Append(name, "opening brief", before); err != nil {
		t.Fatal(err)
	}
	attempt, ok, err := s.sendQueue().ClaimFront(name)
	if err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	if err := s.sendQueue().Resolve(name, attempt, sendq.DefinitelyNotSent, "held at drain: finished and reaped (agent_intent_reaped) — intent says do not deliver (🎯T1066)"); err != nil {
		t.Fatal(err)
	}
	stop := before.Add(time.Hour)
	s.seatStops().Note(seatstop.Record{Seat: name, At: stop, Reason: "stopped"})
	store, err := fleetintent.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.SetFleetIntentStore(store)
	if err := store.SetAgent(name, fleetintent.Reaped, "test", "finished", stop); err != nil {
		t.Fatal(err)
	}
	if got := s.queueStoppageForList(name, time.Now()); got != "" {
		t.Fatalf("held old queue = %q", got)
	}
	// Lifting the hold makes the same entry claimable; an old detail is not
	// itself authority to keep hiding a real delivery obligation.
	if err := store.SetAgent(name, fleetintent.Working, "test", "resume", time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := s.queueStoppageForList(name, time.Now()); !strings.Contains(got, "171h") {
		t.Fatalf("deliverable queue hidden = %q", got)
	}
}
