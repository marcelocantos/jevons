// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/seatstate"
)

func TestT786InterruptedStartFlightReapedOnBoot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	reg, err := claudia.NewRegistry(filepath.Join(stateDir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	const name = "jv-t786-interrupted"
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "sid-t786",
		Provider: claudia.ProviderGrok, AutoStart: true, Purpose: claudia.PurposeWork,
		TargetID: "T786", Parent: "jevons-po",
	}); err != nil {
		t.Fatal(err)
	}
	ledgerPath := filepath.Join(stateDir, startFlightsFile)
	raw, err := json.Marshal(map[string]startFlightRecord{
		name: {StartedAt: time.Now().UTC(), Interrupted: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ledgerPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.SetSendQueueDir(stateDir)
	if reg.Def(name) != nil {
		t.Fatalf("bounce-interrupted mint %q should be reaped on boot, still registered", name)
	}
}

func TestT786InterruptedStartFlightSkippedWhenBriefLanded(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	reg, err := claudia.NewRegistry(filepath.Join(stateDir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	const name = "jv-t786-briefed"
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "sid-briefed",
		Provider: claudia.ProviderGrok, AutoStart: true, Purpose: claudia.PurposeWork,
		Materialized: true, TargetID: "T786", Parent: "jevons-po",
	}); err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			return claudia.StartStub(ctx, cfg, nil)
		},
	})
	if _, err := reg.Launch(name); err != nil {
		t.Fatal(err)
	}
	ledgerPath := filepath.Join(stateDir, startFlightsFile)
	raw, err := json.Marshal(map[string]startFlightRecord{
		name: {StartedAt: time.Now().UTC(), Interrupted: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ledgerPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.SetSendQueueDir(stateDir)
	if reg.Def(name) == nil {
		t.Fatal("materialized alive seat should survive interrupted-start cleanup")
	}
}

func TestT786BornStuckReapedAfterParentNoticed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	reg, err := claudia.NewRegistry(filepath.Join(stateDir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	const worker = "jv-t786-born-stuck"
	if err := reg.Register(claudia.AgentDef{
		Name: worker, WorkDir: dir, SessionID: "sid-born",
		Provider: claudia.ProviderGrok, AutoStart: true, Purpose: claudia.PurposeWork,
		TargetID: "T786", Parent: "jevons-po",
	}); err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.setBirthStoreDir(stateDir)
	key := bornStuckNoticeKey(worker, "sid-born")
	s.markNoticeOutcome(key, true, nil, time.Now())
	s.Seats().Observe(seatstate.Observation{
		Name: worker, SessionID: "sid-born", Alive: seatstate.Yes,
		BornStuck: seatstate.Yes, Source: "test",
	})
	s.sweepBornStuck()
	if reg.Def(worker) != nil {
		t.Fatal("daemon should reap born-stuck seat after parent notice was submitted")
	}
}
