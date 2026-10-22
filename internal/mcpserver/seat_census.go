// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"sort"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/cli"
	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/seatprovider"
	"github.com/marcelocantos/jevons/internal/seatstate"
	"github.com/marcelocantos/jevons/internal/targetfile"
)

// seatCensus is live seats per provider plan plus the names counted (🎯T795).
type seatCensus struct {
	Load  cost.LoadCounts
	Names map[string][]string
}

// harnessLiveSeatCensus is the seat-count / session-census source: positive
// alive evidence only. Stopped, reaped, born-stuck, and other-ledger rows do
// not bind the provider soft cap. Routing still uses harnessLoadCounts, which
// may reserve unknown slots fail-closed.
func (s *Server) harnessLiveSeatCensus() seatCensus {
	if s == nil || s.registry == nil {
		return seatCensus{}
	}
	intent := s.fleetIntent()
	scope := s.workerWD
	return activeLiveSeatCensus(s.registry.List(), func(name string) seatstate.State {
		return seatstate.ReadRegistry(s.registry, name)
	}, func(name string) fleetintent.State {
		return intent.AgentState(name)
	}, scope, func(name string) bool {
		return intent.AgentState(name) != fleetintent.Working && s.registry.Get(name) == nil
	})
}

// activeLiveSeatCensus is a process-free seam for 🎯T795 hermetic tests.
func activeLiveSeatCensus(
	defs []claudia.AgentDef,
	state func(string) seatstate.State,
	intent func(string) fleetintent.State,
	censusWorkdir string,
	parkedWithoutHandle func(string) bool,
) seatCensus {
	out := seatCensus{Load: cost.LoadCounts{}, Names: map[string][]string{}}
	ledger := targetfile.LedgerKey(censusWorkdir)
	for _, d := range defs {
		if d.WorkDir != "" && ledger != "" && !targetfile.SameLedger(ledger, targetfile.LedgerKey(d.WorkDir)) {
			continue
		}
		st := state(d.Name)
		if st.BornStuck == seatstate.Yes {
			continue
		}
		if intent(d.Name) == fleetintent.Reaped {
			continue
		}
		if !seatLiveForCap(st, parkedWithoutHandle(d.Name)) {
			continue
		}
		p := seatprovider.Resolve(d.Provider).Plan
		if p == seatprovider.Unset {
			p = seatprovider.Resolve(cli.DefaultProvider).Plan
		}
		plan := p.String()
		out.Load[plan]++
		out.Names[plan] = append(out.Names[plan], d.Name)
	}
	for plan, names := range out.Names {
		sort.Strings(names)
		out.Names[plan] = names
	}
	return out
}

// seatLiveForCap is true when positive evidence says the seat occupies a
// process slot. Unknown without Yes on any liveness channel does not count
// (🎯T795); that is narrower than activeProcessLoad, which fails closed.
func seatLiveForCap(st seatstate.State, parkedWithoutHandle bool) bool {
	if st.Alive == seatstate.Yes || st.BrokerAlive == seatstate.Yes || st.LocalAlive == seatstate.Yes {
		return true
	}
	if st.Alive == seatstate.No || parkedWithoutHandle {
		return false
	}
	return false
}
