// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/brokerreg"
)

// registryAbsentBrokerSection surfaces live cursor broker seats missing from
// agents.json (🎯T1174 / T861 acceptance b).
func (s *Server) registryAbsentBrokerSection() string {
	if s == nil || s.registry == nil {
		return ""
	}
	broker := readBrokerSeats()
	if broker == nil {
		return ""
	}
	seats := make([]claudia.BrokerSeat, 0, len(broker))
	for _, seat := range broker {
		seats = append(seats, seat)
	}
	rows := brokerreg.RegistryAbsentRows(brokerreg.RegisteredSet(s.registry), seats)
	return brokerreg.FormatRegistryAbsentSection(rows)
}
