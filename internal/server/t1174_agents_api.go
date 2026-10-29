// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/brokerreg"
)

// appendRegistryAbsentBrokerRows surfaces live cursor broker seats that have
// no jevons registry row (🎯T1174 / T861 acceptance b).
func appendRegistryAbsentBrokerRows(ctx context.Context, reg *claudia.Registry, rows []agentInfo) []agentInfo {
	if reg == nil {
		return rows
	}
	seats, err := brokerreg.ListSeats(ctx)
	if err != nil || seats == nil {
		return rows
	}
	absent := brokerreg.RegistryAbsentRows(brokerreg.RegisteredSet(reg), seats)
	if len(absent) == 0 {
		return rows
	}
	out := append([]agentInfo{}, rows...)
	for _, seat := range absent {
		out = append(out, agentInfo{
			Name:     seat.Name,
			Provider: seat.Provider,
			Status:   brokerreg.ListStatusRegistryAbsent,
			Running:  seat.Running,
		})
	}
	return out
}
