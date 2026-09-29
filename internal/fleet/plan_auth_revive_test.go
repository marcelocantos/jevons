// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"errors"
	"reflect"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleetintent"
)

// Once a plan's login is good, the candidates are exactly the stopped seats
// on that plan whose last launch failed on the plan login, and whose intent
// allows revival. A deliberately stopped seat has no recorded failure.
func TestPlanAuthCandidatesArePlanPeersThatBrokeOnLogin(t *testing.T) {
	failures := map[string]string{
		"po-a":      "broker protocol: agent_failed: omp: xai-oauth refresh failed: invalid_grant",
		"po-b":      "omp: keychain item is not the plan blob: invalid character '7'",
		"po-parked": "omp: xai-oauth refresh failed: invalid_grant",
		"po-live":   "omp: xai-oauth refresh failed: invalid_grant",
		"po-crash":  "grok process exited: signal: killed",
		"po-claude": "anthropic refresh failed: invalid_grant",
	}
	for name, why := range failures {
		rehydrateFailures.Store(name, why)
	}
	t.Cleanup(func() {
		for name := range failures {
			rehydrateFailures.Delete(name)
		}
	})
	defs := []claudia.AgentDef{
		{Name: "po-a", Provider: "xai-oauth"},
		{Name: "po-b", Provider: "grok"},
		{Name: "po-parked", Provider: "xai-oauth"},
		{Name: "po-live", Provider: "xai-oauth"},
		{Name: "po-crash", Provider: "xai-oauth"},
		{Name: "po-claude", Provider: "anthropic"},
		{Name: "po-stopped", Provider: "xai-oauth"},
	}
	alive := func(name string) bool { return name == "po-live" }
	intent := fleetintent.Snapshot{Agents: map[string]fleetintent.Record{"po-parked": {State: fleetintent.Parked}}}

	got := planAuthCandidates(defs, alive, intent, "grok")
	if want := []string{"po-a", "po-b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
	if got := planAuthCandidates(defs, alive, intent, "anthropic"); !reflect.DeepEqual(got, []string{"po-claude"}) {
		t.Fatalf("claude candidates = %v", got)
	}
}

// 🎯T884: after a broker restart, the seats to re-attach are the auto-start
// seats with no live handle whose intent allows revival. A running seat, a
// seat that is not auto-start, and a parked or reaped seat are left alone.
func TestReattachCandidatesAreStoppedAutoStartSeatsIntentAllows(t *testing.T) {
	defs := []claudia.AgentDef{
		{Name: "po-down", AutoStart: true},
		{Name: "po-live", AutoStart: true},
		{Name: "po-parked", AutoStart: true},
		{Name: "po-reaped", AutoStart: true},
		{Name: "worker-done"},
	}
	alive := func(name string) bool { return name == "po-live" }
	intent := fleetintent.Snapshot{Agents: map[string]fleetintent.Record{
		"po-parked": {State: fleetintent.Parked},
		"po-reaped": {State: fleetintent.Reaped},
	}}
	if got := reattachCandidates(defs, alive, intent); !reflect.DeepEqual(got, []string{"po-down"}) {
		t.Fatalf("candidates = %v, want [po-down]", got)
	}
}

// 🎯T905: after the owner repairs a plan, its stopped auto-start seats come
// back even when this host recorded no failure for them — the broker could
// not open the plan when it resumed them (2026-09-29: four POs stayed
// stopped after the owner's Reauth). Parked, reaped and other-plan seats,
// and a non-auto-start seat with nothing recorded, stay as they are.
func TestOwnerReauthCandidatesIncludeStoppedAutoStartSeats(t *testing.T) {
	rehydrateFailures.Store("worker-broke", "omp: anthropic refresh failed: invalid_grant")
	t.Cleanup(func() { rehydrateFailures.Delete("worker-broke") })
	defs := []claudia.AgentDef{
		{Name: "po-down", Provider: "anthropic", AutoStart: true},
		{Name: "po-live", Provider: "anthropic", AutoStart: true},
		{Name: "po-parked", Provider: "anthropic", AutoStart: true},
		{Name: "po-reaped", Provider: "anthropic", AutoStart: true},
		{Name: "po-grok", Provider: "xai-oauth", AutoStart: true},
		{Name: "worker-broke", Provider: "anthropic"},
		{Name: "worker-idle", Provider: "anthropic"},
	}
	alive := func(name string) bool { return name == "po-live" }
	intent := fleetintent.Snapshot{Agents: map[string]fleetintent.Record{
		"po-parked": {State: fleetintent.Parked},
		"po-reaped": {State: fleetintent.Reaped},
	}}
	got := ownerReauthCandidates(defs, alive, intent, "anthropic")
	if want := []string{"po-down", "worker-broke"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
	// The standing sweep keeps its narrower rule: recorded failures only.
	if got := planAuthCandidates(defs, alive, intent, "anthropic"); !reflect.DeepEqual(got, []string{"worker-broke"}) {
		t.Fatalf("sweep candidates = %v", got)
	}
}

// 🎯T905: an adopt the broker refused on the plan store is recorded, so the
// healthy-plan sweep revives the seat; a seat simply not running is not.
func TestAdoptFailureOnThePlanLoginIsRecorded(t *testing.T) {
	t.Cleanup(func() { rehydrateFailures.Delete("po-a"); rehydrateFailures.Delete("po-b") })
	noteAdoptFailure("po-a", errors.New("broker protocol: agent_failed: omp: plan data file /x/plan-credentials.enc does not match the Keychain key"))
	noteAdoptFailure("po-b", errors.New("no session window: po-b"))
	defs := []claudia.AgentDef{{Name: "po-a", Provider: "anthropic"}, {Name: "po-b", Provider: "anthropic"}}
	got := planAuthCandidates(defs, func(string) bool { return false }, fleetintent.Snapshot{}, "anthropic")
	if !reflect.DeepEqual(got, []string{"po-a"}) {
		t.Fatalf("sweep candidates = %v, want [po-a]", got)
	}
}
