// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package research

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/capacity"
)

// stubGate admits or defers every ask, and counts the asks.
type stubGate struct {
	mu       sync.Mutex
	decision capacity.Decision
	asks     []string
	released int
}

func (g *stubGate) Begin(class, name string) (capacity.Decision, func()) {
	g.mu.Lock()
	g.asks = append(g.asks, class+":"+name)
	d := g.decision
	g.mu.Unlock()
	d.Class, d.Name = capacity.NormalizeClass(class), name
	return d, func() {
		g.mu.Lock()
		g.released++
		g.mu.Unlock()
	}
}

func (g *stubGate) askCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.asks)
}

func (g *stubGate) releaseCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.released
}

// 🎯T359: a scheduled pass that capacity defers must not run, and the skip
// must be durable — "research went quiet" has to be answerable afterwards.
func TestScheduledCycleDefersUnderCapacityPressure(t *testing.T) {
	gate := &stubGate{decision: capacity.Decision{
		Verdict: capacity.VerdictDefer, Reason: capacity.ReasonCriticalOwnerOnly,
		Detail: "only owner and Build work fits", Pressure: capacity.PressureCritical,
	}}
	ran := make(chan CycleResult, 4)
	agent, _ := testAgent(t, func(a *Args) {
		a.Interval = 30 * time.Millisecond
		a.Capacity = gate
		a.OnResult = func(res CycleResult) {
			select {
			case ran <- res:
			default:
			}
		}
	})

	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		agent.Run(ctx)
	}()
	defer func() {
		cancel()
		<-stopped
	}()

	// Give the schedule several ticks' worth of time; none may produce work.
	deadline := time.After(2 * time.Second)
	for gate.askCount() == 0 {
		select {
		case <-deadline:
			t.Fatal("the schedule never asked capacity for admission")
		case res := <-ran:
			t.Fatalf("a deferred tick ran a cycle anyway: %+v", res)
		case <-time.After(10 * time.Millisecond):
		}
	}
	select {
	case res := <-ran:
		t.Fatalf("a deferred tick ran a cycle anyway: %+v", res)
	case <-time.After(200 * time.Millisecond):
	}

	notes, err := agent.Store().List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(notes) != 0 {
		t.Fatalf("deferred ticks wrote %d note(s); want none", len(notes))
	}
	st, err := agent.State()
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if !strings.Contains(st.LastSkipReason, "capacity") {
		t.Fatalf("skip reason = %q, want it to name capacity", st.LastSkipReason)
	}
}

// An admitted pass runs and releases its slot — a leaked slot would wedge the
// class at its concurrency bound forever.
func TestScheduledCycleRunsAndReleasesWhenAdmitted(t *testing.T) {
	gate := &stubGate{decision: capacity.Decision{
		Verdict: capacity.VerdictAdmit, Tier: capacity.TierFull, Reason: capacity.ReasonHeadroomOK,
	}}
	ran := make(chan CycleResult, 4)
	agent, _ := testAgent(t, func(a *Args) {
		a.Interval = 30 * time.Millisecond
		a.Capacity = gate
		a.OnResult = func(res CycleResult) {
			select {
			case ran <- res:
			default:
			}
		}
	})
	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		agent.Run(ctx)
	}()
	defer func() {
		cancel()
		<-stopped
	}()

	select {
	case res := <-ran:
		if !res.Changed() {
			t.Fatalf("admitted tick produced nothing: %+v", res)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("admitted tick never ran")
	}
	// The release runs after the pass; poll briefly rather than racing it.
	for range 100 {
		if gate.releaseCount() > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("an admitted pass never released its capacity slot")
}

// A degraded pass still refreshes the notes — at half the lookback and half
// the mining bounds. Owner-invoked cycles are never gated at all.
func TestReducedTierStillRefreshesAndManualCyclesBypassTheGate(t *testing.T) {
	gate := &stubGate{decision: capacity.Decision{
		Verdict: capacity.VerdictDegrade, Tier: capacity.TierReduced,
		Reason: capacity.ReasonElevatedDegrade, Pressure: capacity.PressureElevated,
	}}
	agent, _ := testAgent(t, func(a *Args) {
		a.Interval = -1 // no schedule; drive the pass directly
		a.Capacity = gate
	})

	reduced, err := agent.runOnce("test", capacity.TierReduced)
	if err != nil {
		t.Fatalf("reduced runOnce: %v", err)
	}
	if !reduced.Changed() {
		t.Fatalf("a reduced pass must still refresh notes: %+v", reduced)
	}
	if _, err := agent.RunOnce("mcp"); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if gate.askCount() != 0 {
		t.Fatalf("an owner-invoked cycle asked the capacity gate %d time(s)", gate.askCount())
	}
	// Halving must not collapse a bound to zero — a reduced pass is smaller,
	// never blind.
	if reducedFactor(10) != 5 || reducedFactor(1) != 1 || reducedFactor(0) != 0 {
		t.Fatal("reducedFactor must halve without collapsing a bound below 1")
	}
}
