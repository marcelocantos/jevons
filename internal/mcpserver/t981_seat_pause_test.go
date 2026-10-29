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

	"github.com/marcelocantos/jevons/internal/seatstate"
	"github.com/marcelocantos/jevons/internal/turnev"
)

func TestT981ClassifyIdleNudgeSkipsOwnerPausedSeat(t *testing.T) {
	t.Parallel()
	o := IdleNudgeObs{
		Name: "jv-t981", Purpose: claudia.PurposeWork, ProcessRunning: true,
		Phase: "idle", IdleFor: 10 * time.Minute, HasOpenMission: true,
		DeliberateStop: true,
	}
	act, reason := ClassifyIdleNudge(o)
	if act != IdleNudgeSkip || reason != "deliberate_stop" {
		t.Fatalf("owner-paused running seat: %s/%s", act, reason)
	}
}

func TestT981OwnerPauseDeliberateStopWhileRunning(t *testing.T) {
	t.Parallel()
	if !ownerPauseDeliberateStop(true, true, true) {
		t.Fatal("owner pause must deliberate-stop even when AutoStart and running")
	}
	if ownerPauseDeliberateStop(true, true, false) {
		t.Fatal("running AutoStart without owner pause is not deliberate stop")
	}
}

func TestT981PauseHoldsOwnerSend(t *testing.T) {
	reg, err := claudia.NewRegistry(t.TempDir() + "/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	name := "jv-t981-worker"
	dir := t.TempDir()
	if err := reg.Register(claudia.AgentDef{Name: name, WorkDir: dir, SessionID: "s1", Provider: "grok"}); err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg}
	if err := s.PauseSeatOwner(name, "owner"); err != nil {
		t.Fatal(err)
	}
	res, err := s.deliverByName(name, "hello while paused", OriginOwner, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "paused_held" {
		t.Fatalf("status=%q want paused_held", res.Status)
	}
	if !s.SeatOwnerPaused(name) {
		t.Fatal("still paused before unpause")
	}
}

func TestT981UnpauseDeliversResumeAndFlushesHeld(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	const name = "jv-t981-unpause"
	dir := t.TempDir()
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "s1", Provider: claudia.ProviderGrok, Purpose: claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}
	sender := &fakeSender{alive: true}
	sender.sendHook = func() {
		sender.mu.Lock()
		sender.inFlight = false
		sender.mu.Unlock()
	}
	s, _ := chainServer(t, map[string]*fakeSender{name: sender})
	s.registry = reg
	if err := s.PauseSeatOwner(name, "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.deliverByName(name, "held owner note", OriginOwner, false); err != nil {
		t.Fatal(err)
	}
	if err := s.UnpauseSeatOwner(name, "owner"); err != nil {
		t.Fatal(err)
	}
	if s.SeatOwnerPaused(name) {
		t.Fatal("paused mark should be cleared")
	}
	got := sender.sentSnapshot()
	if len(got) < 1 || got[0] != SeatPauseResumeText {
		t.Fatalf("resume not delivered first: %v", got)
	}
	// Held text is delivered on unpause (may queue behind the resume turn).
	queued, err := s.sendQueue().Snapshot(name)
	if err != nil {
		t.Fatal(err)
	}
	if len(queued) == 0 {
		for _, m := range got[1:] {
			if m == "held owner note" {
				return
			}
		}
		t.Fatalf("held owner note neither queued nor sent: sent=%v queue=%v", got, queued)
	}
	if queued[0].Text != "held owner note" {
		t.Fatalf("queue=%v want held owner note first", queued)
	}
}

func TestT981PauseInterruptsOnlyAliveSeat(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	const name = "jv-t981-interrupt"
	dir := t.TempDir()
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "s1", Provider: claudia.ProviderGrok, Purpose: claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}
	var interrupts int
	var mu sync.Mutex
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			return claudia.StartStub(ctx, cfg, &claudia.StubAgentOps{
				Interrupt: func() error {
					mu.Lock()
					interrupts++
					mu.Unlock()
					return nil
				},
			})
		},
	})
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	if _, err := reg.Launch(name); err != nil {
		t.Fatal(err)
	}
	s.Seats().Observe(seatstate.Observation{
		Name: name, Alive: seatstate.Yes, Phase: turnev.PhaseWorking, Source: "test",
	})
	if err := s.PauseSeatOwner(name, "owner"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	n := interrupts
	mu.Unlock()
	if n != 1 {
		t.Fatalf("interrupts=%d want 1", n)
	}
	if !s.SeatOwnerPaused(name) {
		t.Fatal("seat should stay paused until unpause")
	}
}
