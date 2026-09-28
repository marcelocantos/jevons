// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
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
