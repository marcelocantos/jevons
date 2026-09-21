// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/marcelocantos/jevons/internal/planusage"
)

// Default isolate plan feed (🎯T625.6): a weekly window with 90% left,
// which classifies "under" — a destination band. Without it the isolate
// reads the HOST's real plan feed, and an unpinned mint dies on
// plan_dest_empty whenever the host's claude band is ahead.
const (
	defaultPlanRemaining = 90.0
	defaultPlanUsed      = 10.0
	planFixtureFile      = "plan-usage-fixture.json"
)

// planFixtureSnapshot builds a one-backend (grok) weekly snapshot.
func planFixtureSnapshot(rem, used float64) planusage.Snapshot {
	now := time.Now().UTC()
	week := now.Add(3*24*time.Hour + 12*time.Hour)
	lim := planusage.DefaultWeeklyWindowSeconds
	return planusage.Snapshot{
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
}

func writePlanFixtureFile(dir, name string, rem, used float64) (string, error) {
	path := filepath.Join(dir, name)
	b, err := json.Marshal(planFixtureSnapshot(rem, used))
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, b, 0o600)
}

// defaultPlanFixtureEnv writes the default fixture into dir and returns
// the env entry pointing the daemon at it. Callers append it BEFORE any
// per-journey daemonEnv so a journey's own fixture wins.
func defaultPlanFixtureEnv(dir string) (string, error) {
	path, err := writePlanFixtureFile(dir, planFixtureFile, defaultPlanRemaining, defaultPlanUsed)
	if err != nil {
		return "", err
	}
	return planusage.FixtureEnv + "=" + path, nil
}
