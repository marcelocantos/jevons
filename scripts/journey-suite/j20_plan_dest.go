// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/marcelocantos/jevons/internal/planusage"
)

func (s *suite) writePlanFixture(rem, used float64) (string, error) {
	return writePlanFixtureFile(s.stateDir, "plan-usage-j20.json", rem, used)
}

func (s *suite) j20PlanDest() error {
	if s.host == "" || strings.HasSuffix(s.host, ":13705") {
		return fmt.Errorf("J20 refuses development port")
	}
	// Ahead: used 70 => burn 1.4 (ahead is 1.0-1.5; 55 measured ok). No dest → omit-provider must refuse.
	path, err := s.writePlanFixture(30, 70)
	if err != nil {
		return err
	}
	s.daemonEnv = append(s.daemonEnv, planusage.FixtureEnv+"="+path)
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
	if th.AheadRatio != 1.0 || th.HotRatio != 1.5 {
		return fmt.Errorf("thresholds vertices %+v", th)
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
	_, err = s.MCPToolCall("jevons_agent_start", map[string]any{
		"name": "jv-t39015-park", "workdir": s.workdir, "actor": "jevons",
		"parent": "jevons", "purpose": "work", "provider": "grok",
	})
	if err != nil {
		return fmt.Errorf("explicit grok start: %w", err)
	}
	if _, err := s.writePlanFixture(0, 100); err != nil {
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
