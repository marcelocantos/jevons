// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/planusage"
)

func (s *suite) writePlanFixture(rem, used float64) (string, error) {
	return writePlanFixtureFile(s.stateDir, "plan-usage-j20.json", "grok", rem, used)
}

func (s *suite) j20PlanDest() (result error) {
	if s.host == "" || strings.HasSuffix(s.host, ":13705") {
		return fmt.Errorf("J20 refuses development port")
	}
	// Ahead: used 70 => burn 1.4 (ahead is 1.0-1.5; 55 measured ok). No dest → omit-provider must refuse.
	path, err := s.writePlanFixture(30, 70)
	if err != nil {
		return err
	}
	s.daemonEnv = append(s.daemonEnv, planusage.FixtureEnv+"="+path)
	// The daemon keeps reading this fixture for the rest of the run, and
	// J20 leaves grok exhausted. Every later journey then found no plan
	// destination (J21, J35 and J36 in the 2026-10-05 gate). Leave the plan
	// healthy again, and leave no seat parked by it: see restorePlanAfterJ20.
	defer func() { result = errors.Join(result, s.restorePlanAfterJ20(path)) }()
	if err := s.bounceDrain(); err != nil {
		return fmt.Errorf("bounce with fixture: %w", err)
	}

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

	// Mint an explicit grok worker while the feed is a destination band
	// (the product refuses a mint onto an exhausted dest even with an
	// explicit provider), then exhaust the weekly window; sweep parks it.
	if _, err := s.writePlanFixture(defaultPlanRemaining, defaultPlanUsed); err != nil {
		return err
	}
	// The worker hangs off an intermediate seat. When this fixture exhausts
	// grok, that seat is on the hot provider too and the sweep parks it
	// (🎯T850). The oracle here is the worker.
	_, err = s.MCPToolCall("jevons_agent_start", map[string]any{
		"name": "jv-t39015-po", "workdir": s.workdir, "actor": "jevons",
		"parent": "jevons", "purpose": "work", "provider": "grok",
	})
	if err != nil {
		return fmt.Errorf("explicit grok po start: %w", err)
	}
	_, err = s.MCPToolCall("jevons_agent_start", map[string]any{
		"name": "jv-t39015-park", "workdir": s.workdir, "actor": "jv-t39015-po",
		"parent": "jv-t39015-po", "purpose": "work", "provider": "grok",
	})
	if err != nil {
		return fmt.Errorf("explicit grok start: %w", err)
	}
	// Exhaust every plan, not grok alone. With other plans unread, Claudia's
	// placement defers ("destination plan readings incomplete") rather than
	// parking, since a destination may exist; a park needs every plan read
	// and none eligible.
	all, err := json.Marshal(planFixtureSnapshotOf([]string{"grok", "claude", "codex", "cursor"}, 0, 100))
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, all, 0o600); err != nil {
		return err
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
		if a.Name == "jv-t39015-park" && a.To == "" {
			parked = true
		}
	}
	if !parked {
		return fmt.Errorf("sweep did not park jv-t39015-park: %+v", acts)
	}
	return nil
}

// j20PlanParkBy is the actor the plan sweep records on the parks it makes.
const j20PlanParkBy = "product:plan_policy"

// j20RecoverWait bounds how long a recovered plan may take to bring back the
// seats J20's exhausted feed parked.
const j20RecoverWait = 90 * time.Second

// restorePlanAfterJ20 gives the isolate back the way J20 found it.
//
// Exhausting every plan parks every seat on one, the overseer included
// (🎯T850). Restoring the fixture alone left those parks in place until the
// daemon's own periodic sweep reconsidered them, five minutes later. In gate
// 252cf6ac that window held J30's owner turn ("chat: owner turn held by fleet
// intent") past its 90 s paint deadline: J30 failed in the suite and passed
// alone. So the restore sweeps at once and waits until no seat is still
// parked by plan policy. That is also an assertion of the product's recovery
// path: a park the plan forced is reconsidered when the plan recovers.
func (s *suite) restorePlanAfterJ20(path string) error {
	if _, err := s.writePlanFixtureFile(path); err != nil {
		return fmt.Errorf("restore plan fixture: %w", err)
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
	// The worker J20 minted has done its job; a seat left running would do
	// work of its own during every later journey.
	for _, name := range []string{"jv-t39015-park", "jv-t39015-po"} {
		_, _ = s.mcpText("jevons_agent_kill", map[string]any{"name": name, "actor": "jevons", "force": true})
	}
	return nil
}

// writePlanFixtureFile rewrites path with the healthy default feed.
func (s *suite) writePlanFixtureFile(path string) (string, error) {
	got, err := s.writePlanFixture(defaultPlanRemaining, defaultPlanUsed)
	if err == nil && got != path {
		err = fmt.Errorf("plan fixture moved from %s to %s", path, got)
	}
	return got, err
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
