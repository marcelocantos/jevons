// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"

	"github.com/marcelocantos/jevons/internal/cost"
)

// 🎯T708: the governor could not see its own fleet. ActiveSessions came
// from the cost subsystem's billable session list, and under subscription
// accounting that list is empty — so fourteen live seats read as zero and
// every refusal printed "0 live sessions of 20".
func TestT708CensusCountsLiveSeatsWhenCostSessionsAreEmpty(t *testing.T) {
	snap := CapacitySnapshot(CapacitySnapshotArgs{
		Cost: func() (*cost.Snapshot, error) {
			return &cost.Snapshot{Accounting: "subscription", Billable: false}, nil
		},
		ProviderLoad: func() map[string]int {
			return map[string]int{"claude": 12, "cursor": 1, "grok": 1}
		},
	})
	if snap.ActiveSessions != 14 {
		t.Fatalf("active sessions = %d, want the 14 live seats provider load reports", snap.ActiveSessions)
	}
}

// Cost-window sessions remain visible but cannot bind process admission.
func TestT708CensusPrefersBillableSessions(t *testing.T) {
	snap := CapacitySnapshot(CapacitySnapshotArgs{
		Cost: func() (*cost.Snapshot, error) {
			return &cost.Snapshot{
				Accounting: "list_price",
				Billable:   true,
				Sessions:   []cost.BurnRow{{}, {}, {}},
			}, nil
		},
		ProviderLoad: func() map[string]int { return map[string]int{"claude": 12} },
	})
	if snap.ActiveSessions != 12 || snap.CostWindowSessions != 3 {
		t.Fatalf("process seats = %d, cost sessions = %d, want 12 and 3", snap.ActiveSessions, snap.CostWindowSessions)
	}
}

// No load reported anywhere stays zero rather than inventing a census.
func TestT708CensusDoesNotInventSeats(t *testing.T) {
	snap := CapacitySnapshot(CapacitySnapshotArgs{})
	if snap.ActiveSessions != 0 {
		t.Fatalf("active sessions = %d, want 0 with nothing to read", snap.ActiveSessions)
	}
}
