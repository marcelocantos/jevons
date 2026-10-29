// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/capacity"
	"github.com/marcelocantos/jevons/internal/cost"
)

// 🎯T867: seat-count admission binds registered alive fleet processes, not
// cost-window rows with an empty worker field (owner grok sessions outside
// the harness). Incident 2026-09-25: 24 unattributed cost rows refused a
// spawn while only seven fleet seats were alive.
func TestT867SeatCountUsesProcessCensusNotCostWindow(t *testing.T) {
	const maxSessions = 20
	const costRows = 24

	t867CostSnap := func() (*cost.Snapshot, error) {
		rows := make([]cost.BurnRow, costRows)
		for i := range rows {
			rows[i] = cost.BurnRow{
				SessionID: fmt.Sprintf("owner-youtube-%d", i),
				Worker:    "",
				Model:     "grok-4.5-build",
			}
		}
		return &cost.Snapshot{
			Accounting: cost.AccountingSubscription,
			Billable:   false,
			Sessions:   rows,
		}, nil
	}
	snapshot := func(alive int) capacity.Snapshot {
		return CapacitySnapshot(CapacitySnapshotArgs{
			Budget: func() *cost.BudgetConfig {
				return &cost.BudgetConfig{MaxSessions: maxSessions, Accounting: cost.AccountingSubscription}
			},
			Cost:         t867CostSnap,
			ProviderLoad: func() map[string]int { return map[string]int{"grok": alive} },
		})
	}

	snap7 := snapshot(7)
	if snap7.ActiveSessions != 7 || snap7.CostWindowSessions != costRows || snap7.MaxSessions != maxSessions {
		t.Fatalf("census = %+v, want 7 process / %d cost / %d max", snap7, costRows, maxSessions)
	}
	d := capacity.AdmitSpawn(capacity.SpawnWorker, snap7, capacity.DefaultPolicy())
	if !d.Admitted() {
		t.Fatalf("7 live seats with %d unattributed cost rows refused: %+v", costRows, d)
	}

	snap21 := snapshot(21)
	d = capacity.AdmitSpawn(capacity.SpawnWorker, snap21, capacity.DefaultPolicy())
	if d.Admitted() || d.Reason != capacity.ReasonSeatCount {
		t.Fatalf("21 live seats should refuse seat_count: %+v", d)
	}
	if !strings.Contains(d.Detail, "21 live seats of 20") {
		t.Fatalf("refusal must cite registered census, got: %q", d.Detail)
	}
	if strings.Contains(d.Detail, fmt.Sprintf("%d live seats", costRows)) {
		t.Fatalf("refusal must not cite cost-window count: %q", d.Detail)
	}
}

// Production path: jevons_agent_start calls checkHostSpawnAllowed → governor.AdmitSpawn.
func TestT867AgentStartAdmitsUnderCostWindowNoise(t *testing.T) {
	s := &Server{capacityGov: capacity.NewGovernor(capacity.GovernorArgs{
		Snapshot: func() capacity.Snapshot {
			rows := make([]cost.BurnRow, 24)
			return CapacitySnapshot(CapacitySnapshotArgs{
				Budget: func() *cost.BudgetConfig {
					return &cost.BudgetConfig{MaxSessions: 20, Accounting: cost.AccountingSubscription}
				},
				Cost: func() (*cost.Snapshot, error) {
					return &cost.Snapshot{Accounting: cost.AccountingSubscription, Sessions: rows}, nil
				},
				ProviderLoad: func() map[string]int { return map[string]int{"grok": 7} },
			})
		},
	})}
	if blocked := s.checkHostSpawnAllowed("work", "jv-t851-global-rate"); blocked != nil {
		t.Fatalf("spawn refused through host gate: %s", toolText(blocked))
	}
}
