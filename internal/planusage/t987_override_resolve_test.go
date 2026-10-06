// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T987: the owner's keep-off override is an exclusion the resolver
// honours, not a paint job. On 2026-10-02 two bare starts resolved to grok
// while plan-overrides.json held grok exhausted ("quota is dangerously
// low"): claudia.Resolve ranked from the readings, which were green, and
// nothing in the adapter told it otherwise.
func TestT987ResolveMintSkipsOwnerOverriddenExhaustedPlan(t *testing.T) {
	now := time.Date(2026, 10, 2, 11, 0, 0, 0, time.UTC)
	th := DefaultThresholds()
	const reason = "Owner: Grok quota is dangerously low; do not seat work on it."
	grok := t495Backend("grok", t495pf(40), t495pf(60), t495Week(0.5), now)
	claude := t495Backend("claude", t495pf(45), t495pf(55), t495Week(0.5), now)

	// The readings alone would have grok as a destination.
	if !DestEligible(grok, now, th) {
		t.Fatal("fixture: grok's readings must be eligible on their own")
	}
	grok.Override = &Override{Band: BandExhausted, Reason: reason, SetBy: "owner", SetAt: now}
	if DestEligible(grok, now, th) {
		t.Fatal("fixture: the override must make grok ineligible")
	}
	cands := []DestCand{{Provider: "grok", Backend: grok}, {Provider: "claude", Backend: claude}}

	pick, err := ResolveMint(context.Background(), cands, now, th)
	if err != nil {
		t.Fatal(err)
	}
	if pick.Provider != claudia.ProviderClaude {
		t.Fatalf("ResolveMint = %q, want claude (grok is owner-overridden exhausted)", pick.Provider)
	}
	// Migrate / park destinations run through the same seam.
	dest, err := ResolveDest(context.Background(), cands, "", now, th)
	if err != nil {
		t.Fatal(err)
	}
	if dest.Provider != claudia.ProviderClaude {
		t.Fatalf("ResolveDest = %q, want claude", dest.Provider)
	}
	if got, ok := PickPlanDest(cands, now, th); !ok || got != "claude" {
		t.Fatalf("PickPlanDest = %q %v, want claude", got, ok)
	}

	// Without the override the same readings pick grok: the test bites.
	open := []DestCand{{Provider: "grok", Backend: t495Backend("grok", t495pf(40), t495pf(60), t495Week(0.5), now)}}
	if p, err := ResolveMint(context.Background(), open, now, th); err != nil || p.Provider != claudia.ProviderGrok {
		t.Fatalf("control: grok alone without override = %q %v, want grok", p.Provider, err)
	}
}

// 🎯T987: when the override leaves nothing to resolve to, the refusal names
// the owner's override and reason — the T978 shape, not a bare "no catalog
// model matches predicates".
func TestT987ResolveMintRefusalNamesTheOwnerOverride(t *testing.T) {
	now := time.Date(2026, 10, 2, 11, 0, 0, 0, time.UTC)
	th := DefaultThresholds()
	const reason = "Owner: Grok quota is dangerously low."
	grok := t495Backend("grok", t495pf(40), t495pf(60), t495Week(0.5), now)
	grok.Override = &Override{Band: BandExhausted, Reason: reason, SetBy: "owner", SetAt: now}
	cands := []DestCand{{Provider: "grok", Backend: grok}}

	_, err := ResolveMint(context.Background(), cands, now, th)
	if err == nil {
		t.Fatal("ResolveMint picked a plan the owner keeps seats off")
	}
	for _, want := range []string{"owner override: ", "grok (owner override exhausted: " + reason + ")"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal %q does not say %q", err.Error(), want)
		}
	}
	if got := OwnerKeepOffReason(grok); got != "owner override exhausted: "+reason {
		t.Fatalf("OwnerKeepOffReason = %q", got)
	}
}

// 🎯T987 guards 🎯T948: a dest-band override is not an exclusion. A plan
// the owner holds in ok with hot readings is still where a mint lands.
func TestT987DestBandOverrideIsNotAnExclusion(t *testing.T) {
	now := time.Date(2026, 10, 2, 11, 0, 0, 0, time.UTC)
	th := DefaultThresholds()
	claude := t495Backend("claude", t495pf(85), t495pf(15), t495Week(0.5), now)
	if DestEligible(claude, now, th) {
		t.Fatal("fixture: hot claude must be ineligible on its readings")
	}
	claude.Override = &Override{Band: BandOK, Reason: "Owner has a reset available.", SetBy: "owner", SetAt: now}
	if got := OwnerKeepOffReason(claude); got != "" {
		t.Fatalf("a dest-band override reads as keep-off: %q", got)
	}
	grok := t495Backend("grok", t495pf(40), t495pf(60), t495Week(0.5), now)
	cands := []DestCand{{Provider: "grok", Backend: grok}, {Provider: "claude", Backend: claude}}
	// The adapter does not exclude claude; claudia still ranks from the
	// readings and may pass it over, but nothing here refuses it.
	if _, err := ResolveMint(context.Background(), cands, now, th); err != nil {
		t.Fatalf("a dest-band override broke the resolve: %v", err)
	}
	if !IsDestBandOverride(BandOK) || !IsDestBandOverride(BandUnder) || !IsDestBandOverride(BandLocked) {
		t.Fatal("dest bands regressed")
	}
	if IsKeepOffBandOverride(BandOK) || !IsKeepOffBandOverride(BandExhausted) {
		t.Fatal("keep-off band classification is wrong")
	}
}
