// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"strings"
	"testing"
)

// 🎯T762 acceptance 4: a cold-start supervisor reading only /api/agents can
// tell a half-completed order from a completed one — the ordering PO's row
// carries the reconciled order line, and no other row does.
func TestT762AgentRowsCarrySpawnOrders(t *testing.T) {
	s := &Server{}
	s.SetSpawnOrderReader(func(parent string) []string {
		if parent == "jevons-po" {
			return []string{"order o-0635: 1/2 minted (INCOMPLETE); jv-t759 (grok) not_attempted: no jevons_agent_start was made for this seat"}
		}
		return nil
	})
	rows := s.decorateSeatStops([]agentInfo{{Name: "jevons-po", Running: true}, {Name: "jv-t749", Running: true}})
	if len(rows[0].SpawnOrders) != 1 || !strings.Contains(rows[0].SpawnOrders[0], "jv-t759 (grok) not_attempted") {
		t.Fatalf("PO row spawn_orders = %q", rows[0].SpawnOrders)
	}
	if rows[1].SpawnOrders != nil {
		t.Fatalf("worker row decorated: %q", rows[1].SpawnOrders)
	}
	blob, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(blob), `"spawn_orders":["order o-0635: 1/2 minted (INCOMPLETE)`) {
		t.Fatalf("wire lacks spawn_orders: %s", blob)
	}
	if strings.Count(string(blob), "spawn_orders") != 1 {
		t.Fatalf("spawn_orders must be omitted on rows with no open order: %s", blob)
	}
}
