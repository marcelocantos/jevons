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

	"github.com/marcelocantos/jevons/internal/planusage"
	"github.com/marcelocantos/jevons/internal/seatprovider"
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

// Cursor is steerable (claudia v0.62.0 / 🎯T1013.3). The default isolate
// fixture is a single destination backend for the run provider.
func TestCursorFixtureMintsSteerableCompanion(t *testing.T) {
	if planusage.UnsteerableReason("cursor") != "" {
		t.Fatal("cursor is unsteerable; steerable single-backend fixture no longer applies")
	}
	snap := readFixture(t, mustPlanFixtureEnv(t, "cursor"))
	if len(snap.Backends) != 1 || snap.Backends[0].Provider != "cursor" {
		t.Fatalf("cursor fixture backends = %+v, want cursor alone", snap.Backends)
	}
	cands := []planusage.DestCand{{Provider: "cursor", Backend: snap.Backends[0]}}
	pick, err := planusage.ResolveMint(context.Background(), cands, time.Now().UTC(), planusage.DefaultThresholds(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(pick.Provider) != "cursor" {
		t.Fatalf("ResolveMint = %s, want cursor", pick.Provider)
	}
}

// An unsteerable run provider cannot be the mint. The fixture still
// records it, and ResolveMint on that file lands on the steerable
// switch destination rather than tying or returning plan dest empty.
// Real providers are all steerable now; exercise the companion contract
// with SetUnsteerableForTest (same seam as internal/mcpserver 🎯T791).
func TestUnsteerablePinPlanFixtureMintsSteerableCompanion(t *testing.T) {
	t.Cleanup(planusage.SetUnsteerableForTest(map[string]string{"grok": "synthetic exclusion"}))
	pin := "grok"
	snap := readFixture(t, mustPlanFixtureEnv(t, pin))
	if len(snap.Backends) < 2 {
		t.Fatalf("%s fixture backends = %+v, want the pin plus steerable plans", pin, snap.Backends)
	}
	companion := string(seatprovider.Resolve(switchJourneyDestination(claudia.ProviderGrok)).Plan.AsProvider())
	if companion == "" || companion == pin || planusage.UnsteerableReason(companion) != "" {
		t.Fatalf("companion %q is not a steerable plan distinct from %s", companion, pin)
	}
	sawPin, sawCompanion := false, false
	cands := make([]planusage.DestCand, 0, len(snap.Backends))
	for _, be := range snap.Backends {
		if be.Provider == pin {
			sawPin = true
		}
		if be.Provider == companion {
			sawCompanion = true
		}
		cands = append(cands, planusage.DestCand{Provider: be.Provider, Backend: be})
	}
	if !sawPin || !sawCompanion {
		t.Fatalf("%s fixture backends = %+v, want %s and %s", pin, snap.Backends, pin, companion)
	}
	pick, err := planusage.ResolveMint(context.Background(), cands, time.Now().UTC(), planusage.DefaultThresholds(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if planusage.UnsteerableReason(string(pick.Provider)) != "" || string(pick.Provider) == pin {
		t.Fatalf("ResolveMint = %s, want a steerable plan other than %s", pick.Provider, pin)
	}
	if seatprovider.Resolve(pick.Provider).Plan.AsProvider() != claudia.Provider(companion) {
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
	if err := expectLandedProvider(claudia.ProviderCursor, claudia.ProviderCursor); err != nil {
		t.Fatal(err)
	}
	if err := expectLandedProvider(claudia.ProviderCursor, claudia.ProviderCodex); err == nil {
		t.Fatal("steerable cursor accepted a different plan")
	}
}

func TestExpectLandedProviderUnsteerablePin(t *testing.T) {
	t.Cleanup(planusage.SetUnsteerableForTest(map[string]string{"grok": "synthetic exclusion"}))
	if err := expectLandedProvider(claudia.ProviderGrok, claudia.ProviderCodex); err != nil {
		t.Fatal(err)
	}
	if err := expectLandedProvider(claudia.ProviderGrok, claudia.ProviderGrok); err == nil {
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

// 🎯T1234: jevonsd exits at startup on the retired fixture env, so the
// isolate must never inject it by default (every journey died before
// listen while it did).
func TestIsolateEnvCarriesNoRetiredPlanFixture(t *testing.T) {
	t.Setenv(planusage.FixtureEnv, "/retired-fixture-must-not-leak.json")
	s := &suite{stateDir: t.TempDir(), daemonEnv: []string{planusage.FixtureEnv + "=/journey-must-not-win.json"}}
	for _, kv := range s.isolateDaemonEnv() {
		if strings.HasPrefix(kv, planusage.FixtureEnv+"=") {
			t.Fatalf("isolate env carries retired %s: %q", planusage.FixtureEnv, kv)
		}
	}
}
