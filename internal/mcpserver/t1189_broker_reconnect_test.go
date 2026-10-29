// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/seatstate"
	"github.com/marcelocantos/jevons/internal/upgrade"
)

// 🎯T1189: broker-up transition runs an immediate re-attach sweep.
func TestT1189BrokerReconnectWatchRunsSweep(t *testing.T) {
	reg, err := claudia.NewRegistry(t.TempDir() + "/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg}
	var sweeps atomic.Int32
	old := sweepAfterBrokerReconnect
	sweepAfterBrokerReconnect = func(_ *Server, _ context.Context) int {
		sweeps.Add(1)
		return 1
	}
	t.Cleanup(func() { sweepAfterBrokerReconnect = old })

	var mu sync.Mutex
	up := false
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go StartBrokerReconnectWatch(ctx, BrokerReconnectWatchArgs{
		Server: s,
		BrokerUp: func() bool {
			mu.Lock()
			v := up
			mu.Unlock()
			return v
		},
	})

	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	up = true
	mu.Unlock()

	deadline := time.Now().Add(3 * time.Second)
	for sweeps.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if sweeps.Load() == 0 {
		t.Fatal("broker reconnect watch did not run a sweep when the broker came back")
	}
}

// 🎯T1189 acceptance 2/3: process_not_running on send tries re-adopt; failure
// stops the row with a named broker reason instead of leaving it idle.
func TestT1189DeliverWithReadoptStopsSeatWhenReseatFails(t *testing.T) {
	reg, err := claudia.NewRegistry(t.TempDir() + "/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{Name: "w-seat", WorkDir: t.TempDir(), SessionID: "s1", Provider: claudia.ProviderClaude}); err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	s := &Server{registry: reg}
	proc := &claudia.Agent{}
	err = s.deliverWithReadopt("w-seat", proc, func(a agentSender) error {
		return agenterr.WithCause(agenterr.CauseProcessNotRunning, errors.New("claude process not running"))
	})
	if err == nil {
		t.Fatal("deliverWithReadopt: want error when re-seat fails")
	}
	reason, _, ok := s.SeatStopReason("w-seat")
	if !ok || reason != "broker_reconnect_reattach_failed" {
		t.Fatalf("seat stop reason = %q ok=%v after failed adopt", reason, ok)
	}
}

// 🎯T1189 acceptance (hermetic journey): broker returns with a relaunched
// seat still listed idle; a send survives one stale process_not_running via
// re-adopt instead of failing loud to the owner.
func TestT1189HermeticBrokerRestartRelaunchedSeatSend(t *testing.T) {
	const seat = "jv-t1189-worker"
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	reg, err := claudia.NewRegistry(filepath.Join(state, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	stub := func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
		return claudia.StartStub(ctx, cfg, nil)
	}
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: stub,
		Adopt: func(cfg claudia.Config) (*claudia.Agent, error) {
			return stub(context.Background(), cfg)
		},
	})
	if err := reg.Register(claudia.AgentDef{
		Name: seat, WorkDir: dir, SessionID: "s-t1189",
		Provider: claudia.ProviderClaude, AutoStart: true, Purpose: claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	if _, err := reg.Launch(seat); err != nil {
		t.Fatal(err)
	}
	stale := reg.Get(seat)

	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.Seats().Observe(seatstate.Observation{
		Name: seat, Alive: seatstate.Yes, LocalAlive: seatstate.Yes,
		QueueDepth: seatstate.QueueUnknown, Source: "test.t1189",
	})

	prevList := brokerSeatsList
	prevAvail := brokerSeatsAvailable
	brokerSeatsList = func(context.Context) ([]claudia.BrokerSeat, error) {
		return []claudia.BrokerSeat{{Name: seat, Alive: true}}, nil
	}
	brokerSeatsAvailable = func() bool { return true }
	t.Cleanup(func() {
		brokerSeatsList = prevList
		brokerSeatsAvailable = prevAvail
	})

	upgrade.DefaultReadopter = upgrade.NewReadopter(reg)
	t.Cleanup(func() { upgrade.DefaultReadopter = nil })
	if n := s.SweepAfterBrokerReconnect(context.Background()); n == 0 {
		t.Fatal("broker reconnect sweep did not touch the relaunched seat")
	}

	attempts := 0
	err = s.deliverWithReadopt(seat, stale, func(a agentSender) error {
		attempts++
		if attempts == 1 {
			return agenterr.WithCause(agenterr.CauseProcessNotRunning, errors.New("claude process not running"))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("send after broker restart: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("send attempts = %d, want stale-handle failure then success after re-adopt", attempts)
	}
	if _, _, ok := s.SeatStopReason(seat); ok {
		t.Fatal("idle relaunched seat must not show a broker fault after a successful send")
	}
}
