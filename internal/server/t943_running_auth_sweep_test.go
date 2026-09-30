// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T943: a running seat refused on a revoked plan token does not retry its
// own login, so nothing repairs it once a sibling seat's success has already
// cleared the plan for everyone else — the standing sweep must reach it the
// same way the owner's Reauth click does (🎯T905), without waiting for a
// click or relaunching the seat. On 2026-09-30 jevons-po sat "running idle"
// for twelve minutes after jevons and claudia-po completed real turns on the
// same anthropic plan.
func TestT943RunningSeatRefusedOnStaleTokenIsRecoveredBySweep(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	const refused, healthy, other = "t943-refused", "t943-healthy", "t943-grok"
	for name, provider := range map[string]claudia.Provider{refused: "anthropic", healthy: "anthropic", other: "grok"} {
		if err := reg.Register(claudia.AgentDef{Name: name, WorkDir: t.TempDir(), Provider: provider, SessionID: name + "-sid"}); err != nil {
			t.Fatal(err)
		}
	}
	var launched []string
	reg.SetLaunchers(&claudia.RegistryLaunchers{Start: func(_ context.Context, cfg claudia.Config) (*claudia.Agent, error) {
		launched = append(launched, cfg.Name)
		return claudia.NewStubAgent(nil), nil
	}})
	for _, name := range []string{refused, healthy, other} {
		if _, err := reg.Launch(name); err != nil {
			t.Fatal(err)
		}
	}
	launched = nil

	s := New("test", t.TempDir())
	s.SetRegistry(reg)
	// A stateful stub, matching how the real idleActivity tracker behaves:
	// ClearPlanAuthFailures actually answers PlanAuthFailed's next call.
	failed := map[string]bool{refused: true}
	var cleared []string
	s.SetPlanAuthFailure(
		func(name string) bool { return failed[name] },
		func(names []string) {
			cleared = append(cleared, names...)
			for _, n := range names {
				failed[n] = false
			}
		},
	)
	recovered := 0
	s.authRecover = func(_ context.Context, provider claudia.Provider) error {
		recovered++
		if provider != "anthropic" {
			t.Fatalf("provider = %q", provider)
		}
		return nil
	}

	// No owner click, no HTTP request: the standing sweep alone finds the
	// refused running seat and repairs it in place.
	got := s.RecoverRunningPlanAuthFailures(context.Background())
	if recovered != 1 {
		t.Fatalf("recoverAuth calls = %d, want 1", recovered)
	}
	if len(launched) != 0 {
		t.Fatalf("a running seat was relaunched: %v", launched)
	}
	slices.Sort(got)
	if !slices.Equal(got, []string{refused}) {
		t.Fatalf("recovered = %v, want [%s]", got, refused)
	}
	slices.Sort(cleared)
	if !slices.Equal(cleared, []string{healthy, refused}) {
		t.Fatalf("cleared = %v, want both seats on the anthropic plan and not the grok one", cleared)
	}

	// The refusal is answered: a second sweep finds nothing left to recover.
	if got := s.RecoverRunningPlanAuthFailures(context.Background()); len(got) != 0 || recovered != 1 {
		t.Fatalf("second sweep should be a no-op once the seat is cleared: got=%v recoverAuth calls=%d", got, recovered)
	}
}

// A plan that is still broken is retried on the cooldown, not once per tick.
func TestT943RunningSweepCooldownsAFailedRecovery(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	const refused = "t943-cooldown-refused"
	if err := reg.Register(claudia.AgentDef{Name: refused, WorkDir: t.TempDir(), Provider: "openai-codex", SessionID: "sid"}); err != nil {
		t.Fatal(err)
	}
	reg.SetLaunchers(&claudia.RegistryLaunchers{Start: func(_ context.Context, _ claudia.Config) (*claudia.Agent, error) {
		return claudia.NewStubAgent(nil), nil
	}})
	if _, err := reg.Launch(refused); err != nil {
		t.Fatal(err)
	}

	s := New("test", t.TempDir())
	s.SetRegistry(reg)
	s.SetPlanAuthFailure(func(name string) bool { return name == refused }, func([]string) {})
	recoverCalls := 0
	s.authRecover = func(context.Context, claudia.Provider) error {
		recoverCalls++
		return fmt.Errorf("still broken")
	}
	if got := s.RecoverRunningPlanAuthFailures(context.Background()); len(got) != 0 || recoverCalls != 1 {
		t.Fatalf("first sweep: got=%v recoverAuth calls=%d, want 0 recovered / 1 call", got, recoverCalls)
	}
	// Same tick, plan still broken: the cooldown holds off the retry.
	if got := s.RecoverRunningPlanAuthFailures(context.Background()); len(got) != 0 || recoverCalls != 1 {
		t.Fatalf("second sweep inside cooldown: got=%v recoverAuth calls=%d, want no extra call", got, recoverCalls)
	}
}

// A stopped seat is RevivePlanAuthWhereHealthy's job, not this sweep's: this
// sweep only ever touches seats it finds still alive.
func TestT943RunningSweepSkipsStoppedSeats(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	if err := reg.Register(claudia.AgentDef{Name: "t943-stopped", WorkDir: t.TempDir(), Provider: "anthropic", SessionID: "sid"}); err != nil {
		t.Fatal(err)
	}
	// Never launched: reg.Get returns nil, so the seat is not alive.

	s := New("test", t.TempDir())
	s.SetRegistry(reg)
	s.SetPlanAuthFailure(func(string) bool { return true }, func([]string) {})
	recoverCalls := 0
	s.authRecover = func(context.Context, claudia.Provider) error {
		recoverCalls++
		return nil
	}
	if got := s.RecoverRunningPlanAuthFailures(context.Background()); len(got) != 0 || recoverCalls != 0 {
		t.Fatalf("stopped seat should not be touched: got=%v recoverAuth calls=%d", got, recoverCalls)
	}
}
