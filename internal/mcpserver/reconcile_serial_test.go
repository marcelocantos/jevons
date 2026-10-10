// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/seatstate"
	"github.com/marcelocantos/jevons/internal/staffops"
)

// The sentinel repair request and cockpit hook use the same entry point.
// An overlapping request joins the in-flight pass, not a second actuator run.
func TestReconcileConcurrentRequestsJoinOnePass(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	entered := make(chan struct{})
	release := make(chan struct{})
	var passes atomic.Int32
	s.reconcilePassHook = func() { passes.Add(1); close(entered); <-release }
	cockpitDone := make(chan struct{})
	go func() { s.Reconcile(); close(cockpitDone) }()
	<-entered
	joined := make(chan struct{})
	s.reconcileJoinHook = func() { close(joined) }
	sentinelDone := make(chan struct{})
	go func() { s.Reconcile(); close(sentinelDone) }()
	<-joined // The second request has observed the in-flight pass.
	select {
	case <-sentinelDone:
		t.Fatal("sentinel returned before the cockpit pass completed")
	default:
	}
	if got := passes.Load(); got != 1 {
		t.Fatalf("overlapping passes: %d", got)
	}
	close(release)
	<-cockpitDone
	<-sentinelDone
	if got := passes.Load(); got != 1 {
		t.Fatalf("duplicate pass: %d", got)
	}
	// A later tick is still allowed: exclusion is not a permanent throttle.
	s.reconcilePassHook = func() { passes.Add(1) }
	s.Reconcile()
	if got := passes.Load(); got != 2 {
		t.Fatalf("cadence lost after join: %d", got)
	}
}

// An actual classified sentinel ActionRepair joins the cockpit's in-flight
// pass, rather than starting another one. The next cockpit tick still runs.
func TestSentinelRepairJoinsCockpitPass(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{Name: "jevons", SessionID: "dead-session", Purpose: claudia.PurposeOverseer}); err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.observeRegistryLiveness()
	s.Seats().Observe(seatstate.Observation{Name: "jevons", Alive: seatstate.No, QueueDepth: seatstate.QueueUnknown, Source: "test.overseer-down"})
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	rt := s.ensureSentinelRuntime(10)
	rt.mu.Lock()
	rt.firstSeen["overseer:down"] = now.Add(-10 * time.Minute)
	rt.mu.Unlock()
	entered, release, joined := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var passes atomic.Int32
	s.reconcilePassHook = func() { passes.Add(1); close(entered); <-release }
	s.reconcileJoinHook = func() { close(joined) }
	cockpitDone := make(chan struct{})
	go func() { s.Reconcile(); close(cockpitDone) }()
	<-entered
	sentinelDone := make(chan struct{})
	var primary staffops.Action
	var repaired bool
	go func() {
		res, act := s.runSentinelCycle(SentinelLoopArgs{Server: s, Now: func() time.Time { return now }})
		primary, repaired = res.Primary, act.Repaired
		close(sentinelDone)
	}()
	select {
	case <-joined:
	case <-time.After(3 * time.Second):
		close(release)
		<-cockpitDone
		<-sentinelDone
		t.Fatalf("sentinel did not request the running pass: primary=%s repaired=%v", primary, repaired)
	}
	if passes.Load() != 1 {
		t.Fatal("overlapping repair pass")
	}
	close(release)
	<-cockpitDone
	<-sentinelDone
	if primary != staffops.ActionRepair || !repaired || passes.Load() != 1 {
		t.Fatalf("sentinel primary=%s repaired=%v passes=%d", primary, repaired, passes.Load())
	}
	s.reconcilePassHook = func() { passes.Add(1) }
	s.Reconcile()
	if passes.Load() != 2 {
		t.Fatalf("subsequent cockpit cadence lost: %d", passes.Load())
	}
}

// A sentinel symptom under a deliberate provider wall does not turn a
// request for the shared pass into an intent bypass.
func TestSentinelBlockedIntentDoesNotRequestRepair(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{Name: "jevons", SessionID: "dead-session", Purpose: claudia.PurposeOverseer}); err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.observeRegistryLiveness()
	s.Seats().Observe(seatstate.Observation{Name: "jevons", Alive: seatstate.No, QueueDepth: seatstate.QueueUnknown, Source: "test.overseer-down"})
	store, err := fleetintent.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.SetFleetIntentStore(store)
	if err := s.SetFleetIntent(fleetintent.BlockedProvider, "test", "provider wall"); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	rt := s.ensureSentinelRuntime(10)
	rt.mu.Lock()
	rt.firstSeen["overseer:down"] = now.Add(-10 * time.Minute)
	rt.mu.Unlock()
	var passes atomic.Int32
	s.reconcilePassHook = func() { passes.Add(1) }
	res, act := s.runSentinelCycle(SentinelLoopArgs{Server: s, Now: func() time.Time { return now }})
	if res.Primary == staffops.ActionRepair || act.Repaired || passes.Load() != 0 {
		t.Fatalf("provider wall bypass: primary=%s repaired=%v passes=%d", res.Primary, act.Repaired, passes.Load())
	}
}
