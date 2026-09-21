// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/planusage"
)

func bandOfFixture(t *testing.T, rem, used float64) planusage.BandInfo {
	t.Helper()
	snap := planFixtureSnapshot(rem, used)
	return planusage.WeeklyBandDetail(snap.Backends[0], time.Now().UTC(), planusage.DefaultThresholds())
}

// The default isolate feed must be a destination band, or an unpinned mint
// is refused plan_dest_empty.
func TestDefaultPlanFixtureIsDestination(t *testing.T) {
	bi := bandOfFixture(t, defaultPlanRemaining, defaultPlanUsed)
	if !bi.Eligible {
		t.Fatalf("default fixture band %q not a destination", bi.Band)
	}
}

// Control: J20's refusal fixture must NOT be a destination, else J20's
// "omit-provider must refuse" assertion could not fail on a product that
// lets a mint go to an ahead/hot destination.
func TestJ20FixtureIsAheadNotDestination(t *testing.T) {
	bi := bandOfFixture(t, 30, 70)
	if bi.Eligible || bi.Band != planusage.BandAhead {
		t.Fatalf("J20 fixture band=%q eligible=%v, want ahead/ineligible", bi.Band, bi.Eligible)
	}
}

func TestIsolateEnvCarriesPlanFixtureAndJourneyOverrides(t *testing.T) {
	s := &suite{stateDir: t.TempDir(), daemonEnv: []string{planusage.FixtureEnv + "=/j20"}}
	var vals []string
	for _, kv := range s.isolateDaemonEnv() {
		if strings.HasPrefix(kv, planusage.FixtureEnv+"=") {
			vals = append(vals, kv)
		}
	}
	if len(vals) < 2 || !strings.Contains(vals[len(vals)-2], s.stateDir) || vals[len(vals)-1] != planusage.FixtureEnv+"=/j20" {
		t.Fatalf("fixture env order wrong: %v", vals)
	}
}
