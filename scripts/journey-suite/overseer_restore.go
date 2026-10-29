// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/marcelocantos/jevons/internal/seatprovider"
)

// overseerRestoreBudget bounds the switch back, which waits for any turn in
// flight to settle first.
const overseerRestoreBudget = 2 * time.Minute

// restoreOverseerProvider puts the shared overseer back on the suite's
// provider after a journey moved it (🎯T1301). J13 switches it to another
// backend and J20's exhaust-and-recover re-mints it; before this the next
// journeys ran on whatever was left. 🎯T832 made their checks follow the
// overseer's current provider, but J45's recovery depends on the backend it
// starts from: on 2026-10-29 the fast gate ran it after J13 had left the
// overseer on anthropic, the recovery landed on xai-oauth, the hold never
// began, and J33 and J52 failed behind it. Each journey now starts from the
// suite's provider. A refused switch is reported, not scored: the next
// journey's own checks still judge the seat it finds.
func (s *suite) restoreOverseerProvider(after string) {
	if s.tornDown || s.isolateDown != nil {
		return
	}
	rows, err := bounceRegistrySnapshot(s.agentsPath())
	if err != nil {
		return
	}
	row, ok := rows[overseerName]
	if !ok || row.Provider == "" {
		return
	}
	want := seatprovider.Resolve(s.provider).Plan
	if seatprovider.Resolve(row.Provider).Plan == want {
		return
	}
	body, err := json.Marshal(map[string]any{"provider": string(s.provider)})
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), overseerRestoreBudget)
	defer cancel()
	if err := postSwitchWhenSettled(ctx, "http://"+s.host+"/api/agents/switch-model", body); err != nil {
		fmt.Fprintf(s.out(), "NOTE after %s: %s stays on %s; switching back to %s failed: %v\n", after, overseerName, row.Provider, s.provider, err)
		return
	}
	fmt.Fprintf(s.out(), "NOTE after %s: %s was on %s; switched back to %s (🎯T1301)\n", after, overseerName, row.Provider, s.provider)
}
