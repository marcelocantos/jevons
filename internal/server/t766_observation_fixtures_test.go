// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleetlog"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

func listObservedFleetModels(reg *claudia.Registry, account *fleetlog.Account, onRecovered func([]string), progress *AgentProgressHub, models *fleetModelResolver) []agentInfo {
	a := seatstate.RegistryAuthority(reg)
	a.ObserveRegistry(reg)
	for _, d := range reg.List() {
		observeSeatModel(a, d, progress, models)
	}
	return listFleetAgentsNotifying(reg, account, onRecovered, progress, models)
}

// Sessionless transport stubs do not provide production identity evidence.
// These auth fixtures explicitly declare which handles were launched.
func observeLaunchedStubSeats(s *Server, reg *claudia.Registry) {
	for _, d := range reg.List() {
		if reg.Get(d.Name) != nil {
			s.seats.Load().FromClaudia(seatstate.SeatReport{Name: d.Name, SessionID: d.SessionID, Provider: string(d.Provider), Alive: true, Known: true}, time.Now())
		}
	}
}
