// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/cli"
	"github.com/marcelocantos/jevons/internal/planusage"
)

func bandOfFixture(t *testing.T, provider string, rem, used float64) planusage.BandInfo {
	t.Helper()
	snap := planFixtureSnapshot(provider, rem, used)
	return planusage.WeeklyBandDetail(snap.Backends[0], time.Now().UTC(), planusage.DefaultThresholds())
}

// The default isolate feed must be a destination band, or an unpinned mint
// is refused plan_dest_empty.
func TestDefaultPlanFixtureIsDestination(t *testing.T) {
	for _, p := range []string{"claude", "grok"} {
		bi := bandOfFixture(t, p, defaultPlanRemaining, defaultPlanUsed)
		if !bi.Eligible {
			t.Fatalf("%s default fixture band %q not a destination", p, bi.Band)
		}
	}
}

// The default env fixture follows the run's provider: a PROVIDER=claude run
// must offer a claude destination, not grok.
func TestDefaultPlanFixtureFollowsProvider(t *testing.T) {
	for _, p := range []string{"claude", "grok", "codex"} {
		env, err := defaultPlanFixtureEnv(t.TempDir(), p)
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(strings.TrimPrefix(env, planusage.FixtureEnv+"="))
		if err != nil {
			t.Fatal(err)
		}
		var snap planusage.Snapshot
		if err := json.Unmarshal(b, &snap); err != nil {
			t.Fatal(err)
		}
		if len(snap.Backends) != 1 || snap.Backends[0].Provider != p {
			t.Fatalf("fixture for %s has backends %+v", p, snap.Backends)
		}
	}
}

func readFixture(t *testing.T, env string) planusage.Snapshot {
	t.Helper()
	b, err := os.ReadFile(strings.TrimPrefix(env, planusage.FixtureEnv+"="))
	if err != nil {
		t.Fatal(err)
	}
	var snap planusage.Snapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		t.Fatal(err)
	}
	return snap
}

// An unsteerable run provider cannot be the mint. The fixture still
// records it, and ResolveMint on that file lands on the steerable
// switch destination rather than tying or returning plan dest empty.
func TestCursorFixtureMintsSteerableCompanion(t *testing.T) {
	if planusage.UnsteerableReason("cursor") == "" {
		t.Fatal("cursor is steerable; this fixture's companion contract no longer applies")
	}
	snap := readFixture(t, mustPlanFixtureEnv(t, "cursor"))
	if len(snap.Backends) < 2 {
		t.Fatalf("cursor fixture backends = %+v, want the pin plus steerable plans", snap.Backends)
	}
	companion := string(cli.PlanProvider(switchJourneyDestination(claudia.ProviderCursor)))
	if companion == "" || companion == "cursor" || planusage.UnsteerableReason(companion) != "" {
		t.Fatalf("companion %q is not a steerable plan distinct from cursor", companion)
	}
	sawCursor, sawCompanion := false, false
	cands := make([]planusage.DestCand, 0, len(snap.Backends))
	for _, be := range snap.Backends {
		if be.Provider == "cursor" {
			sawCursor = true
		}
		if be.Provider == companion {
			sawCompanion = true
		}
		cands = append(cands, planusage.DestCand{Provider: be.Provider, Backend: be})
	}
	if !sawCursor || !sawCompanion {
		t.Fatalf("cursor fixture backends = %+v, want cursor and %s", snap.Backends, companion)
	}
	pick, err := planusage.ResolveMint(context.Background(), cands, time.Now().UTC(), planusage.DefaultThresholds())
	if err != nil {
		t.Fatal(err)
	}
	if planusage.UnsteerableReason(string(pick.Provider)) != "" || string(pick.Provider) == "cursor" {
		t.Fatalf("ResolveMint = %s, want a steerable plan other than cursor", pick.Provider)
	}
	if cli.PlanProvider(pick.Provider) != claudia.Provider(companion) {
		t.Fatalf("ResolveMint = %s, want companion %s", pick.Provider, companion)
	}
}

func mustPlanFixtureEnv(t *testing.T, provider string) string {
	t.Helper()
	env, err := defaultPlanFixtureEnv(t.TempDir(), provider)
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func TestExpectLandedProvider(t *testing.T) {
	if err := expectLandedProvider(claudia.ProviderClaude, claudia.ProviderClaude); err != nil {
		t.Fatal(err)
	}
	if err := expectLandedProvider(claudia.ProviderGrok, "xai-oauth"); err != nil {
		t.Fatal(err)
	}
	if err := expectLandedProvider(claudia.ProviderClaude, claudia.ProviderCodex); err == nil {
		t.Fatal("steerable request accepted a different plan")
	}
	if err := expectLandedProvider(claudia.ProviderCursor, ""); err == nil {
		t.Fatal("empty landing accepted")
	}
	if planusage.UnsteerableReason("cursor") == "" {
		t.Fatal("cursor is steerable; the unsteerable landing cases are vacuous")
	}
	if err := expectLandedProvider(claudia.ProviderCursor, claudia.ProviderCodex); err != nil {
		t.Fatal(err)
	}
	if err := expectLandedProvider(claudia.ProviderCursor, claudia.ProviderCursor); err == nil {
		t.Fatal("unsteerable pin accepted itself")
	}
}

// Control: J20's refusal fixture must NOT be a destination, else J20's
// "omit-provider must refuse" assertion could not fail on a product that
// lets a mint go to an ahead/hot destination.
func TestJ20FixtureIsAheadNotDestination(t *testing.T) {
	bi := bandOfFixture(t, "grok", 30, 70)
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
