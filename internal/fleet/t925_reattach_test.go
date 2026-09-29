// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"errors"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleetintent"
)

type t925Reg struct {
	defs      []claudia.AgentDef
	running   map[string]bool // the broker still runs it: adopt works
	alive     map[string]bool
	adopts    []string
	relaunchs []string
}

func (r *t925Reg) List() []claudia.AgentDef { return r.defs }
func (r *t925Reg) Alive(name string) bool   { return r.alive[name] }
func (r *t925Reg) Adopt(name string) error {
	r.adopts = append(r.adopts, name)
	if !r.running[name] {
		return errors.New("broker: no such grant")
	}
	r.alive[name] = true
	return nil
}
func (r *t925Reg) Relaunch(name string) error {
	r.relaunchs = append(r.relaunchs, name)
	r.alive[name] = true
	return nil
}

// 🎯T925: after a broker restart, a seat this host lost to the broker comes
// back — adopted when the broker relaunched it, relaunched on its own
// conversation when it did not — work seat or not. A seat stopped for any
// other reason is only adopted, never relaunched here, and intent still
// wins: a parked seat is untouched even though the broker took it down.
func TestT925SeatsLostToABrokerRestartComeBack(t *testing.T) {
	names := []string{"worker-lost", "worker-lost-running", "po-lost-parked", "po-paused", "po-running"}
	t.Cleanup(func() {
		for _, n := range names {
			brokerReattachTried.Delete(n)
		}
	})
	reg := &t925Reg{
		defs: []claudia.AgentDef{
			{Name: "worker-lost", Purpose: claudia.PurposeWork},
			{Name: "worker-lost-running", Purpose: claudia.PurposeWork},
			{Name: "po-lost-parked", AutoStart: true},
			{Name: "po-paused", AutoStart: true},
			{Name: "po-running", AutoStart: true},
		},
		running: map[string]bool{"worker-lost-running": true, "po-running": true},
		alive:   map[string]bool{},
	}
	lost := map[string]bool{"worker-lost": true, "worker-lost-running": true, "po-lost-parked": true}
	intent := fleetintent.Snapshot{Agents: map[string]fleetintent.Record{"po-lost-parked": {State: fleetintent.Parked}}}
	now := time.Date(2026, 9, 29, 20, 35, 0, 0, time.UTC)

	got := reattachWith(reg, intent, now, func(n string) bool { return lost[n] })
	sort.Strings(got)
	if want := []string{"po-running", "worker-lost", "worker-lost-running"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("back = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(reg.relaunchs, []string{"worker-lost"}) {
		t.Fatalf("relaunched %v, want only the lost seat the broker did not bring back", reg.relaunchs)
	}
	for _, n := range reg.adopts {
		if n == "po-lost-parked" {
			t.Fatal("a parked seat was touched")
		}
	}
	if reg.alive["po-paused"] {
		t.Fatal("a seat stopped on purpose was brought back")
	}
}
