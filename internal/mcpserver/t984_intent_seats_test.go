// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleetintent"
)

// 🎯T984: the 2026-10-01 shape. A parked seat the broker runs unowned, a
// parked seat this daemon holds, a parked seat that is down, a working seat
// the broker runs unknown to this daemon, a seat waiting on the owner.
func TestT984SeatViewsNameEveryFault(t *testing.T) {
	intent := fleetintent.Snapshot{Agents: map[string]fleetintent.Record{
		"parked-broker": {State: fleetintent.Parked},
		"parked-local":  {State: fleetintent.Parked},
		"parked-down":   {State: fleetintent.Parked},
		"owner-wait":    {State: fleetintent.BlockedOwner},
	}}
	names := []string{"parked-broker", "parked-local", "parked-down", "lost", "owner-wait", "fine"}
	local := map[string]bool{"parked-local": true, "owner-wait": true, "fine": true}
	broker := map[string]claudia.BrokerSeat{
		"parked-broker": {Name: "parked-broker", Alive: true},
		"parked-local":  {Name: "parked-local", Alive: true, Owned: true},
		"parked-down":   {Name: "parked-down"},
		"lost":          {Name: "lost", Alive: true},
		"owner-wait":    {Name: "owner-wait", Alive: true, Owned: true},
		"fine":          {Name: "fine", Alive: true, Owned: true},
	}
	alive := func(n string) bool { return local[n] }

	got := seatsAgainstIntent(names, alive, (&Server{}).observeBrokerSeats(broker), intent)
	want := []seatAgainstIntent{
		{Name: "parked-broker", State: fleetintent.Parked, Broker: true},
		{Name: "parked-local", State: fleetintent.Parked, Local: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("against intent = %+v, want %+v", got, want)
	}
	if got := seatsDiverged(names, alive, (&Server{}).observeBrokerSeats(broker), intent); !reflect.DeepEqual(got, []string{"lost"}) {
		t.Fatalf("diverged = %v, want [lost]", got)
	}
	// An unreadable broker is unknown, not "nothing runs": only local
	// processes are judged and nothing is called diverged.
	if got := seatsAgainstIntent(names, alive, nil, intent); len(got) != 1 || got[0].Name != "parked-local" {
		t.Fatalf("against intent with no broker view = %+v", got)
	}
	if got := seatsDiverged(names, alive, nil, intent); len(got) != 0 {
		t.Fatalf("diverged with no broker view = %v", got)
	}
}

// 🎯T984: a parked seat the broker runs unowned is stopped through the broker.
func TestT984ParkedBrokerSeatIsStopped(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"parked-broker", "fine"} {
		if err := reg.Register(claudia.AgentDef{Name: name, WorkDir: dir, SessionID: "s-" + name, Provider: "anthropic", Parent: "jevons-po"}); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{registry: reg}
	st, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.SetFleetIntentStore(st)
	if err := s.SetAgentIntent("parked-broker", fleetintent.Parked, "jevons-po", "finished"); err != nil {
		t.Fatal(err)
	}
	var stopped []string
	prev := brokerSeatStop
	brokerSeatStop = func(_ context.Context, name string) error { stopped = append(stopped, name); return nil }
	t.Cleanup(func() { brokerSeatStop = prev })

	s.stopSeatsAgainstIntent(map[string]claudia.BrokerSeat{
		"parked-broker": {Name: "parked-broker", Alive: true},
		"fine":          {Name: "fine", Alive: true},
	})
	if !reflect.DeepEqual(stopped, []string{"parked-broker"}) {
		t.Fatalf("stopped %v, want only the parked seat", stopped)
	}
}
