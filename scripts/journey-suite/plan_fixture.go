// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/cli"
	"github.com/marcelocantos/jevons/internal/planusage"
)

// Default isolate plan feed (🎯T625.6): a weekly window with 90% left,
// which classifies "under" — a destination band. Without it the isolate
// reads the HOST's real plan feed, and an unpinned mint dies on
// plan_dest_empty whenever the host's claude band is ahead.
//
// A steerable run provider is the only backend. An unsteerable run
// provider stays in the snapshot (the journey asked for it) and the
// fixture also offers steerable plans. The switch destination sits at
// strictly more weekly headroom than the others: equal slack is
// ErrTokenTied, and ResolveMint then refuses every seat.
const (
	defaultPlanRemaining    = 90.0
	defaultPlanUsed         = 10.0
	companionPlanRemaining  = 95.0
	companionPlanUsed       = 5.0
	otherSteerableRemaining = 80.0
	otherSteerableUsed      = 20.0
	planFixtureFile         = "plan-usage-fixture.json"
)

// steerablePlanNames are the published plans a mint may land on when
// the run provider cannot be steered. Order is only a fallback when
// the switch destination itself is unsteerable.
var steerablePlanNames = []string{"claude", "codex", "grok"}

// planFixtureSnapshot builds a one-backend weekly snapshot for provider.
func planFixtureSnapshot(provider string, rem, used float64) planusage.Snapshot {
	return planFixtureSnapshotOf([]string{provider}, rem, used)
}

// planFixtureSnapshotOf gives every named plan the same weekly reading.
func planFixtureSnapshotOf(providers []string, rem, used float64) planusage.Snapshot {
	now := time.Now().UTC()
	week := now.Add(3*24*time.Hour + 12*time.Hour)
	lim := planusage.DefaultWeeklyWindowSeconds
	snap := planusage.Snapshot{At: now}
	for _, provider := range providers {
		snap.Backends = append(snap.Backends, planusage.Backend{
			Provider: provider,
			Status:   planusage.StatusAvailable,
			Windows: []planusage.Window{{
				Name:               planusage.WindowWeekly,
				RemainingPercent:   &rem,
				UsedPercent:        &used,
				ResetsAt:           &week,
				LimitWindowSeconds: &lim,
			}},
		})
	}
	return snap
}

type planReading struct {
	provider  string
	rem, used float64
}

// isolatePlanReadings is the default snapshot for one journey run.
// A steerable provider is alone, at the destination band. An
// unsteerable provider is recorded and accompanied by steerable plans
// whose weekly slack is not a tie.
func isolatePlanReadings(run string) []planReading {
	run = strings.ToLower(strings.TrimSpace(run))
	own := planReading{provider: run, rem: defaultPlanRemaining, used: defaultPlanUsed}
	if planusage.UnsteerableReason(run) == "" {
		return []planReading{own}
	}
	readings := []planReading{own}
	companion := steerableCompanion(run)
	if companion != "" {
		readings = append(readings, planReading{
			provider: companion, rem: companionPlanRemaining, used: companionPlanUsed,
		})
	}
	for _, p := range steerablePlanNames {
		if p == run || p == companion || planusage.UnsteerableReason(p) != "" {
			continue
		}
		readings = append(readings, planReading{
			provider: p, rem: otherSteerableRemaining, used: otherSteerableUsed,
		})
	}
	return readings
}

// steerableCompanion is the plan an unsteerable run should mint onto:
// the journey's switch destination when that plan can be steered,
// otherwise claude, then codex.
func steerableCompanion(run string) string {
	dest := string(cli.PlanProvider(switchJourneyDestination(claudia.Provider(run))))
	if dest != run && planusage.UnsteerableReason(dest) == "" {
		return dest
	}
	for _, p := range []string{"claude", "codex"} {
		if p != run && planusage.UnsteerableReason(p) == "" {
			return p
		}
	}
	return ""
}

func planFixtureSnapshotRanked(run string) planusage.Snapshot {
	now := time.Now().UTC()
	week := now.Add(3*24*time.Hour + 12*time.Hour)
	lim := planusage.DefaultWeeklyWindowSeconds
	snap := planusage.Snapshot{At: now}
	for _, row := range isolatePlanReadings(run) {
		rem, used := row.rem, row.used
		snap.Backends = append(snap.Backends, planusage.Backend{
			Provider: row.provider,
			Status:   planusage.StatusAvailable,
			Windows: []planusage.Window{{
				Name:               planusage.WindowWeekly,
				RemainingPercent:   &rem,
				UsedPercent:        &used,
				ResetsAt:           &week,
				LimitWindowSeconds: &lim,
			}},
		})
	}
	return snap
}

func writePlanFixtureFile(dir, name, provider string, rem, used float64) (string, error) {
	path := filepath.Join(dir, name)
	b, err := json.Marshal(planFixtureSnapshot(provider, rem, used))
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, b, 0o600)
}

func writePlanSnapshotFile(dir string, snap planusage.Snapshot) (string, error) {
	path := filepath.Join(dir, planFixtureFile)
	b, err := json.Marshal(snap)
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, b, 0o600)
}

// defaultPlanFixtureEnv writes the default fixture into dir and returns
// the env entry pointing the daemon at it. Callers append it BEFORE any
// per-journey daemonEnv so a journey's own fixture wins.
func defaultPlanFixtureEnv(dir, provider string) (string, error) {
	path, err := writePlanSnapshotFile(dir, planFixtureSnapshotRanked(provider))
	if err != nil {
		return "", err
	}
	return planusage.FixtureEnv + "=" + path, nil
}

// expectLandedProvider checks the seat a mint actually started. A
// steerable request must land on that plan. An unsteerable request is
// dropped (🎯T791), so the seat must land on some steerable plan.
func expectLandedProvider(requested, got claudia.Provider) error {
	if strings.TrimSpace(string(got)) == "" {
		return fmt.Errorf("landed provider is empty")
	}
	if planusage.UnsteerableReason(string(requested)) == "" {
		if cli.PlanProvider(got) != cli.PlanProvider(requested) {
			return fmt.Errorf("steerable seat landed on %s, want %s", got, requested)
		}
		return nil
	}
	if planusage.UnsteerableReason(string(got)) != "" {
		return fmt.Errorf("unsteerable pin %s landed on unsteerable %s", requested, got)
	}
	return nil
}
