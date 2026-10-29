// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package brokerreg compares the claudia broker's seat list to jevons'
// agents.json registry (🎯T1174 / T861 acceptance b).
package brokerreg

import (
	"context"
	"sort"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/seatprovider"
)

// ListStatusRegistryAbsent is the /api/agents status for a live broker seat
// with no jevons registry row (🎯T1174).
const ListStatusRegistryAbsent = "registry_absent"

const listSeatsTimeout = 5 * time.Second

// RegistryAbsentRow is one live broker seat with no jevons registry row,
// already folded for API/list surfaces (🎯T1174).
type RegistryAbsentRow struct {
	Name     string
	Provider string
	Running  bool
}

// RegistryAbsentRows maps [AbsentCursorAlive] into plain rows for callers
// outside the broker observation feed.
func RegistryAbsentRows(registered map[string]struct{}, broker []claudia.BrokerSeat) []RegistryAbsentRow {
	seats := AbsentCursorAlive(registered, broker)
	out := make([]RegistryAbsentRow, 0, len(seats))
	for _, seat := range seats {
		out = append(out, RegistryAbsentRow{
			Name:     seat.Name,
			Provider: string(seatprovider.Resolve(seat.Provider).Launch),
			Running:  seat.Alive,
		})
	}
	return out
}

// RegisteredSet is every name in the jevons registry file.
func RegisteredSet(reg *claudia.Registry) map[string]struct{} {
	out := map[string]struct{}{}
	if reg == nil {
		return out
	}
	for _, d := range reg.List() {
		if d.Name != "" {
			out[d.Name] = struct{}{}
		}
	}
	return out
}

// AbsentAlive returns broker seats that are alive but not registered.
func AbsentAlive(registered map[string]struct{}, broker []claudia.BrokerSeat) []claudia.BrokerSeat {
	var out []claudia.BrokerSeat
	for _, seat := range broker {
		if !seat.Alive || seat.Name == "" {
			continue
		}
		if _, ok := registered[seat.Name]; ok {
			continue
		}
		out = append(out, seat)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// AbsentCursorAlive is [AbsentAlive] restricted to cursor provider seats.
func AbsentCursorAlive(registered map[string]struct{}, broker []claudia.BrokerSeat) []claudia.BrokerSeat {
	all := AbsentAlive(registered, broker)
	var out []claudia.BrokerSeat
	for _, seat := range all {
		if seatprovider.Resolve(seat.Provider).Plan == seatprovider.Cursor {
			out = append(out, seat)
		}
	}
	return out
}

// FormatAgentListSection names broker-held cursor seats missing from the
// registry. Empty when there is nothing to report.
func FormatAgentListSection(seats []claudia.BrokerSeat) string {
	if len(seats) == 0 {
		return ""
	}
	rows := make([]RegistryAbsentRow, len(seats))
	for i, seat := range seats {
		rows[i] = RegistryAbsentRow{
			Name:     seat.Name,
			Provider: string(seatprovider.Resolve(seat.Provider).Launch),
			Running:  seat.Alive,
		}
	}
	return FormatRegistryAbsentSection(rows)
}

// ListSeats reads the broker's grant list. Nil, nil means the broker is
// unavailable or could not be read — not "no seats".
func ListSeats(ctx context.Context) ([]claudia.BrokerSeat, error) {
	if !claudia.BrokerAvailable() {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cctx, cancel := context.WithTimeout(ctx, listSeatsTimeout)
	defer cancel()
	return claudia.BrokerSeats(cctx)
}
