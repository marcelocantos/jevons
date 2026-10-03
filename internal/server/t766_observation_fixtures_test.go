// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
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
