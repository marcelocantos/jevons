// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/planusage"
)

// 🎯T1305: a plan-policy park ends the seat's running turn before it stops
// the seat. On 2026-10-29 (J45, isolate jevons-journey-3447837583) a park
// released a broker seat mid-turn: Jevons logged seat_stop ok and a switch
// to anthropic, while the sidecar kept the turn and kept calling the
// exhausted xai-oauth plan for three more minutes. The broker's release did
// not abort the turn; an interrupt does.
func TestT1305PlanPolicyParkInterruptsTheRunningTurnBeforeStopping(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{Name: "worker", WorkDir: dir, SessionID: "s1", Provider: claudia.ProviderClaude, Purpose: claudia.PurposeWork, Materialized: true}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	interrupts, runningAtInterrupt := 0, false
	reg.SetDirect(true)
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: func(context.Context, claudia.Config) (*claudia.Agent, error) {
			return claudia.NewStubAgentOps(&claudia.StubAgentOps{
				Interrupt: func() error {
					mu.Lock()
					interrupts++
					runningAtInterrupt = reg.Get("worker") != nil
					mu.Unlock()
					return nil
				},
				PromptInFlight: func() bool { return true },
			}), nil
		},
	})
	if _, err := reg.Launch("worker"); err != nil {
		t.Fatalf("launch: %v", err)
	}
	t.Cleanup(func() { reg.Stop("worker") })

	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	store, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.SetFleetIntentStore(store)
	s.SetModelSwitcher(&recoverySwitcher{registry: reg})
	snap := planusage.Snapshot{}
	s.SetPlanUsageSource(func() planusage.Snapshot { return snap })
	snap.At = time.Now()
	// Every plan exhausted: no destination, so the sweep parks the seat.
	snap.Backends = []planusage.Backend{
		t39015Weekly("claude", 0, 100, snap.At), t39015Weekly("codex", 0, 100, snap.At),
		t39015Weekly("grok", 0, 100, snap.At), t39015Weekly("cursor", 0, 100, snap.At),
	}

	acts := s.SweepPlanPolicy()
	if len(acts) != 1 || acts[0].Execution != "parked" {
		t.Fatalf("sweep = %+v, want the worker parked", acts)
	}
	mu.Lock()
	n, running := interrupts, runningAtInterrupt
	mu.Unlock()
	if n != 1 || !running {
		t.Fatalf("park interrupted %d time(s), seat running at the interrupt=%v; want one interrupt before the stop", n, running)
	}
	if reg.Get("worker") != nil {
		t.Fatal("the parked seat is still running")
	}
}
