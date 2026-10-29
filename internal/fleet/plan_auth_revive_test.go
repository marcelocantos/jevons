// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/fleetintent"
)

// Once a plan's login is good, the candidates are exactly the stopped seats
// on that plan whose last launch failed on the plan login, and whose intent
// allows revival. A deliberately stopped seat has no recorded failure.
func TestPlanAuthCandidatesArePlanPeersThatBrokeOnLogin(t *testing.T) {
	auth := func(text string) error {
		return agenterr.WithCause(agenterr.CauseAuth, errors.New(text))
	}
	failures := map[string]error{
		"po-a":        auth("broker protocol: agent_failed: omp: xai-oauth refresh failed: invalid_grant"),
		"po-b":        auth("omp: keychain item is not the plan blob: invalid character '7'"),
		"po-parked":   auth("omp: xai-oauth refresh failed: invalid_grant"),
		"po-live":     auth("omp: xai-oauth refresh failed: invalid_grant"),
		"po-crash":    errors.New("grok process exited: signal: killed"),
		"po-claude":   auth("anthropic refresh failed: invalid_grant"),
		"po-sentence": errors.New("omp: xai-oauth refresh failed: invalid_grant"),
	}
	for name, err := range failures {
		RecordRehydrateFailure(name, err)
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
		{Name: "po-sentence", Provider: "xai-oauth"},
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
	if got := reattachCandidates(defs, alive, intent, nil, nil); !reflect.DeepEqual(got, []string{"po-down"}) {
		t.Fatalf("candidates = %v, want [po-down]", got)
	}
}

// 🎯T905: after the owner repairs a plan, its stopped auto-start seats come
// back even when this host recorded no failure for them — the broker could
// not open the plan when it resumed them (2026-09-29: four POs stayed
// stopped after the owner's Reauth). Parked, reaped and other-plan seats,
// and a non-auto-start seat with nothing recorded, stay as they are.
func TestOwnerReauthCandidatesIncludeStoppedAutoStartSeats(t *testing.T) {
	RecordRehydrateFailure("worker-broke", agenterr.WithCause(agenterr.CauseAuth, errors.New("omp: anthropic refresh failed: invalid_grant")))
	RecordRehydrateFailure("worker-sentence", errors.New("omp: anthropic refresh failed: invalid_grant"))
	t.Cleanup(func() {
		rehydrateFailures.Delete("worker-broke")
		rehydrateFailures.Delete("worker-sentence")
	})
	defs := []claudia.AgentDef{
		{Name: "po-down", Provider: "anthropic", AutoStart: true},
		{Name: "po-live", Provider: "anthropic", AutoStart: true},
		{Name: "po-parked", Provider: "anthropic", AutoStart: true},
		{Name: "po-reaped", Provider: "anthropic", AutoStart: true},
		{Name: "po-grok", Provider: "xai-oauth", AutoStart: true},
		{Name: "worker-broke", Provider: "anthropic"},
		{Name: "worker-sentence", Provider: "anthropic"},
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
	t.Cleanup(func() {
		rehydrateFailures.Delete("po-a")
		rehydrateFailures.Delete("po-sentence")
		rehydrateFailures.Delete("po-b")
	})
	keychain := errors.New("broker protocol: agent_failed: omp: plan data file /x/plan-credentials.enc does not match the Keychain key")
	noteAdoptFailure("po-a", agenterr.WithCause(agenterr.CauseAuth, keychain))
	noteAdoptFailure("po-sentence", keychain)
	noteAdoptFailure("po-b", errors.New("no session window: po-b"))
	defs := []claudia.AgentDef{
		{Name: "po-a", Provider: "anthropic"},
		{Name: "po-sentence", Provider: "anthropic"},
		{Name: "po-b", Provider: "anthropic"},
	}
	got := planAuthCandidates(defs, func(string) bool { return false }, fleetintent.Snapshot{}, "anthropic")
	if !reflect.DeepEqual(got, []string{"po-a"}) {
		t.Fatalf("sweep candidates = %v, want [po-a]", got)
	}
}

// A file written before the cause field still shows the sentence on the
// fleet row. The sentence is not a plan-login cause, so the sweep does
// not revive that seat.
func TestLegacyRehydrateSentenceDoesNotRevive(t *testing.T) {
	const name = "legacy-auth"
	path := filepath.Join(t.TempDir(), "rehydrate-failures.json")
	body := []byte("{\n  \"legacy-auth\": \"omp: xai-oauth refresh failed: invalid_grant\"\n}\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = UseRehydrateFailureFile("")
		rehydrateFailures.Delete(name)
	})
	if err := UseRehydrateFailureFile(path); err != nil {
		t.Fatal(err)
	}
	const want = "broken: omp: xai-oauth refresh failed: invalid_grant"
	if got := RehydrateHealth(claudia.AgentDef{Name: name}); got != want {
		t.Fatalf("health = %q, want %q", got, want)
	}
	defs := []claudia.AgentDef{{Name: name, Provider: "xai-oauth"}}
	alive := func(string) bool { return false }
	if got := planAuthCandidates(defs, alive, fleetintent.Snapshot{}, "grok"); len(got) != 0 {
		t.Fatalf("legacy sentence revived %v", got)
	}
	RecordRehydrateFailure(name, agenterr.WithCause(agenterr.CauseAuth, errors.New("omp: xai-oauth refresh failed: invalid_grant")))
	rehydrateFailures.Delete(name)
	if err := UseRehydrateFailureFile(path); err != nil {
		t.Fatal(err)
	}
	if got := planAuthCandidates(defs, alive, fleetintent.Snapshot{}, "grok"); !reflect.DeepEqual(got, []string{name}) {
		t.Fatalf("reloaded typed cause = %v", got)
	}
	if got := RehydrateHealth(claudia.AgentDef{Name: name}); got != want {
		t.Fatalf("health after reload = %q, want %q", got, want)
	}
}
