// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/planusage"
)

func (s *suite) j20PlanDest() (result error) {
	if s.host == "" || strings.HasSuffix(s.host, ":13705") {
		return fmt.Errorf("J20 refuses development port")
	}
	if err := s.ensureIsolatedBroker(); err != nil {
		return err
	}
	// Ahead grok (used 70 => burn 1.4) plus the other three exhausted:
	// no dest → omit-provider must refuse. Seeded through Claudia T234
	// on the live isolate broker — not the retired fixture env.
	if err := s.pushAndAwait(destEmptyOverrideReadings()); err != nil {
		return fmt.Errorf("seed dest-empty usage: %w", err)
	}
	// J20 leaves grok exhausted. Every later journey then found no plan
	// destination (J21, J35 and J36 in the 2026-10-05 gate). Leave the plan
	// healthy again, and leave no seat parked by it: see restorePlanAfterJ20.
	defer func() { result = errors.Join(result, s.restorePlanAfterJ20()) }()

	resp, err := http.Get("http://" + s.host + "/api/plan-usage/thresholds")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("thresholds HTTP %d", resp.StatusCode)
	}
	var th planusage.Thresholds
	if err := json.NewDecoder(resp.Body).Decode(&th); err != nil {
		return err
	}
	// 🎯T1072: the served vertices are Claudia's, not a copy.
	if th != planusage.DefaultThresholds() {
		return fmt.Errorf("thresholds vertices %+v, want Claudia's %+v", th, planusage.DefaultThresholds())
	}

	_, err = s.MCPToolCall("jevons_agent_start", map[string]any{
		"name": "jv-t39015-omit", "workdir": s.workdir, "actor": "jevons",
		"parent": "jevons", "purpose": "work",
	})
	if err == nil || !strings.Contains(err.Error(), "plan dest empty") {
		return fmt.Errorf("omit-provider mint should refuse dest-empty, err=%v", err)
	}

	// Isolate overseer is already up. Confirm the plan-usage tool.
	if _, err := s.MCPToolCall("jevons_plan_usage", nil); err != nil {
		return fmt.Errorf("jevons_plan_usage: %w", err)
	}

	// Mint an explicit grok worker while grok is a destination band
	// (the product refuses a mint onto an exhausted dest even with an
	// explicit provider), then exhaust the weekly window; sweep parks it.
	if err := s.pushAndAwait(grokDestOverrideReadings()); err != nil {
		return fmt.Errorf("seed grok dest usage: %w", err)
	}
	// The worker hangs off an intermediate seat. When this fixture exhausts
	// grok, that seat is on the hot provider too and the sweep parks it
	// (🎯T850). The oracle here is the worker.
	poName, parkName := s.j20SeatNames()
	_, err = s.MCPToolCall("jevons_agent_start", map[string]any{
		"name": poName, "workdir": s.workdir, "actor": "jevons",
		"parent": "jevons", "purpose": "work", "provider": "grok",
	})
	if err != nil {
		return fmt.Errorf("explicit grok po start: %w", err)
	}
	_, err = s.MCPToolCall("jevons_agent_start", map[string]any{
		"name": parkName, "workdir": s.workdir, "actor": poName,
		"parent": poName, "purpose": "work", "provider": "grok",
	})
	if err != nil {
		return fmt.Errorf("explicit grok start: %w", err)
	}
	if s.j45HoldOverseer {
		// 🎯T1091: the park lands while the overseer works a fleet turn.
		if err := s.j45HoldOverseerInFleetTurn(poName); err != nil {
			return err
		}
	}
	// Exhaust every plan, not grok alone. With other plans unread, Claudia's
	// placement defers ("destination plan readings incomplete") rather than
	// parking, since a destination may exist; a park needs every plan read
	// and none eligible.
	if err := s.pushAndAwait(exhaustedOverrideReadings()); err != nil {
		return fmt.Errorf("seed exhausted usage: %w", err)
	}
	sweep, err := http.Post("http://"+s.host+"/api/plan-usage/sweep", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		return err
	}
	defer sweep.Body.Close()
	if sweep.StatusCode != 200 {
		return fmt.Errorf("sweep HTTP %d", sweep.StatusCode)
	}
	var acts []planusage.PlanAction
	if err := json.NewDecoder(sweep.Body).Decode(&acts); err != nil {
		return fmt.Errorf("sweep decode: %w", err)
	}
	parked := false
	for _, a := range acts {
		if a.Name == parkName && a.To == "" {
			parked = true
		}
	}
	if !parked {
		return fmt.Errorf("sweep did not park %s: %+v", parkName, acts)
	}
	return nil
}

// j20SeatNames names the seats J20's exhaust-and-recover mints. J45 runs the
// same flow later in the suite, and a killed seat's stored identity outlives
// the kill in Claudia: re-minting the same name failed with "load seat
// identity mismatch" (final suite 2026-10-20, after J20's recovery had
// re-placed jv-t39015-po). J45's run gets names of its own.
func (s *suite) j20SeatNames() (po, park string) {
	if s.j45HoldOverseer {
		return "jv-t39015-po-j45", "jv-t39015-park-j45"
	}
	return "jv-t39015-po", "jv-t39015-park"
}

// j20PlanParkBy is the actor the plan sweep records on the parks it makes.
const j20PlanParkBy = "product:plan_policy"

// j20RecoverWait bounds how long a recovered plan may take to bring back the
// seats J20's exhausted feed parked.
const j20RecoverWait = 90 * time.Second

// restorePlanAfterJ20 gives the isolate back the way J20 found it.
//
// Exhausting every plan parks every seat on one, the overseer included
// (🎯T850). Restoring the readings alone left those parks in place until the
// daemon's own periodic sweep reconsidered them, five minutes later. In gate
// 252cf6ac that window held J30's owner turn ("chat: owner turn held by fleet
// intent") past its 90 s paint deadline: J30 failed in the suite and passed
// alone. So the restore sweeps at once and waits until no seat is still
// parked by plan policy. That is also an assertion of the product's recovery
// path: a park the plan forced is reconsidered when the plan recovers.
func (s *suite) restorePlanAfterJ20() error {
	if err := s.pushAndAwait(healthyOverrideReadings()); err != nil {
		return fmt.Errorf("restore plan usage: %w", err)
	}
	deadline := time.Now().Add(j20RecoverWait)
	for {
		sweep, err := http.Post("http://"+s.host+"/api/plan-usage/sweep", "application/json", bytes.NewReader([]byte("{}")))
		if err != nil {
			return fmt.Errorf("recovery sweep: %w", err)
		}
		_ = sweep.Body.Close()
		if sweep.StatusCode != http.StatusOK {
			return fmt.Errorf("recovery sweep HTTP %d", sweep.StatusCode)
		}
		parked, err := s.planPolicyParked()
		if err != nil {
			return err
		}
		if len(parked) == 0 {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the plan recovered but these seats are still parked by plan policy after %s: %s",
				j20RecoverWait, strings.Join(parked, ", "))
		}
		time.Sleep(2 * time.Second)
	}
	// J45 must exercise a fleet turn on the recovered overseer. The turn
	// started before exhaustion was interrupted by the plan-policy park;
	// its old process cannot be assumed to resume after the re-mint.
	// Start a fresh held fleet turn after recovery, before removing its sender.
	if s.j45HoldOverseer {
		poName, _ := s.j20SeatNames()
		if err := s.j45HoldOverseerInFleetTurn(poName); err != nil {
			return fmt.Errorf("hold recovered overseer for J45: %w", err)
		}
	}
	// The worker J20 minted has done its job; a seat left running would do
	// work of its own during every later journey.
	poName, parkName := s.j20SeatNames()
	for _, name := range []string{parkName, poName} {
		_, _ = s.mcpText("jevons_agent_kill", map[string]any{"name": name, "actor": "jevons", "force": true})
	}
	// Recovery re-mints the parked overseer, and the new seat may still be
	// working whatever the park interrupted (a research brief, in the
	// 2026-10-20 green run). Hand the next journey an idle overseer — except
	// to J45, which is about exactly that window (🎯T1091).
	if s.j45HoldOverseer {
		return nil
	}
	if err := s.waitAgentPhase(overseerName, func(p string) bool { return p == "idle" }, overseerIdleBefore); err != nil {
		return fmt.Errorf("the overseer J20's recovery brought back never went idle: %w", err)
	}
	return nil
}

// planPolicyParked names the seats whose recorded intent is a plan-policy park.
func (s *suite) planPolicyParked() ([]string, error) {
	store, err := fleetintent.Open(s.stateDir)
	if err != nil {
		return nil, fmt.Errorf("read fleet intent: %w", err)
	}
	var parked []string
	for name, rec := range store.Snapshot().Agents {
		if rec.By == j20PlanParkBy && fleetintent.Resolve(rec.State) == fleetintent.Parked {
			parked = append(parked, name)
		}
	}
	sort.Strings(parked)
	return parked, nil
}
