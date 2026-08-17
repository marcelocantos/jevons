// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/planusage"
)

func (s *suite) writePlanFixture(rem, used float64) (string, error) {
	now := time.Now().UTC()
	week := now.Add(3*24*time.Hour + 12*time.Hour)
	lim := planusage.DefaultWeeklyWindowSeconds
	snap := planusage.Snapshot{
		At: now,
		Backends: []planusage.Backend{{
			Provider: "grok",
			Status:   planusage.StatusAvailable,
			Windows: []planusage.Window{{
				Name:               planusage.WindowWeekly,
				RemainingPercent:   &rem,
				UsedPercent:        &used,
				ResetsAt:           &week,
				LimitWindowSeconds: &lim,
			}},
		}},
	}
	path := filepath.Join(s.stateDir, "plan-usage-fixture.json")
	b, err := json.Marshal(snap)
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, b, 0o600)
}

func (s *suite) j20PlanDest() error {
	if s.host == "" || strings.HasSuffix(s.host, ":13705") {
		return fmt.Errorf("J20 refuses daily port")
	}
	// Ahead: burn 55/50 = 1.1. No dest → omit-provider must refuse.
	path, err := s.writePlanFixture(45, 55)
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

	// Exhausted weekly: explicit grok worker then sweep parks.
	if _, err := s.writePlanFixture(0, 100); err != nil {
		return err
	}
	_, err = s.MCPToolCall("jevons_agent_start", map[string]any{
		"name": "jv-t39015-park", "workdir": s.workdir, "actor": "jevons",
		"parent": "jevons", "purpose": "work", "provider": "grok",
	})
	if err != nil {
		return fmt.Errorf("explicit grok start: %w", err)
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
