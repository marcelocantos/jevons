// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/converge"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/seatstate"
	"github.com/marcelocantos/jevons/internal/turnev"
)

// 🎯T959: plan login recovery re-drives every cleared auth refusal on a
// running seat without waiting for the impatience ladder.

func TestT959PlanLoginRecoveryRedrivesRefusedSeat(t *testing.T) {
	const worker = "jv-t959-worker"
	workerProc := &fakeSender{alive: true}
	s, _ := chainServer(t, map[string]*fakeSender{worker: workerProc})
	s.idleActivity = NewIdleActivityTracker()
	s.Seats().Observe(seatstate.Observation{
		Name: worker, Alive: seatstate.Yes, Phase: turnev.PhaseIdle, Source: "test",
	})

	var redriven []string
	var mu sync.Mutex
	s.planAuthRedriveFn = func(name string) {
		mu.Lock()
		redriven = append(redriven, name)
		mu.Unlock()
	}

	revoked := `provider refused the turn: 401 {"type":"error","error":{"type":"authentication_error","message":"OAuth access token has been revoked"}}`
	s.idleActivity.NoteTerminalTurn(worker, revoked, 0)
	s.idleActivity.NoteTurnErrored(worker, true)
	s.idleActivity.mu.Lock()
	act := s.idleActivity.by[worker]
	act.FailureClass = agenterr.ClassAuth
	s.idleActivity.by[worker] = act
	s.idleActivity.mu.Unlock()

	s.ClearPlanAuthFailures([]string{worker})

	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(redriven)
		mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("plan auth redrive did not run")
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	got := redriven
	mu.Unlock()
	if len(got) != 1 || got[0] != worker {
		t.Fatalf("redriven=%v want [%s]", got, worker)
	}
}

func TestT959PlanLoginRecoveryRedriveSkippedUnderAuthHardBlock(t *testing.T) {
	s, _, _, _ := t968AuthBlock(t)
	s.idleActivity = NewIdleActivityTracker()
	redrives := 0
	s.planAuthRedriveFn = func(string) { redrives++ }
	s.idleActivity.mu.Lock()
	if s.idleActivity.by == nil {
		s.idleActivity.by = make(map[string]IdleActivity)
	}
	s.idleActivity.by["jevons-po"] = IdleActivity{FailureClass: agenterr.ClassAuth}
	s.idleActivity.mu.Unlock()
	s.ClearPlanAuthFailures([]string{"jevons-po"})
	if redrives != 0 {
		t.Fatalf("redrive under hard-block: %d want 0 (T968 owns the probe)", redrives)
	}
	if got := s.fleetIntent().FleetState(); got != fleetintent.BlockedProvider {
		t.Fatalf("fleet=%q", got)
	}
}

func TestT959PlanLoginRecoveryRedriveSubmitsOnRunningSeat(t *testing.T) {
	const worker = "jv-t959-live"
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{Name: worker, WorkDir: dir, Provider: "anthropic", SessionID: "sid", Materialized: true}); err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	proc := &fakeSender{alive: true}
	reg.SetLaunchers(&claudia.RegistryLaunchers{Start: func(context.Context, claudia.Config) (*claudia.Agent, error) {
		return claudia.NewStubAgent(nil), nil
	}})
	if _, err := reg.Launch(worker); err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg}
	s.idleActivity = NewIdleActivityTracker()
	setObservedSenderResolver(s, func(name string) (agentSender, bool, error) {
		if name != worker {
			return nil, false, nil
		}
		return proc, false, nil
	})
	s.Seats().Observe(seatstate.Observation{
		Name: worker, Alive: seatstate.Yes, Phase: turnev.PhaseIdle, Source: "test",
	})
	s.idleActivity.mu.Lock()
	if s.idleActivity.by == nil {
		s.idleActivity.by = make(map[string]IdleActivity)
	}
	s.idleActivity.by[worker] = IdleActivity{FailureClass: agenterr.ClassAuth}
	s.idleActivity.mu.Unlock()

	s.onPlanLoginRecoveredRedrive([]string{worker})
	waitFor(t, "redrive submit", func() bool { return len(proc.sentSnapshot()) > 0 })
	if proc.sentSnapshot()[0] != "" {
		t.Fatalf("redrive submit=%q want empty handoff", proc.sentSnapshot()[0])
	}
}

// Hermetic 401 → plan-login recovery → second turn: provider refused once,
// then recovery re-drives with the refused turn's pending work still claimable.
func TestT959Auth401ThenRecoveryRedrivesWithPendingWork(t *testing.T) {
	const worker = "jv-t959-401ok"
	const pendingPayload = "pending work from the refused turn"
	proc := &fakeSender{alive: true}
	s, _ := chainServer(t, map[string]*fakeSender{worker: proc})
	s.idleActivity = NewIdleActivityTracker()
	s.Seats().Observe(seatstate.Observation{
		Name: worker, Alive: seatstate.Yes, Phase: turnev.PhaseIdle, Source: "test",
	})
	if _, _, err := s.sendQueue().Append(worker, pendingPayload, time.Now()); err != nil {
		t.Fatal(err)
	}
	if n := s.pendingAgentSends(worker); n != 1 {
		t.Fatalf("pending=%d want 1", n)
	}

	revoked := `provider refused the turn: 401 {"type":"error","error":{"type":"authentication_error","message":"OAuth access token has been revoked"}}`
	sink := s.agentEventSink(worker)
	sink(claudia.Event{
		Type: "assistant", Text: revoked, StopReason: "end_turn",
		IsError: true, Reason: agenterr.CauseAuth,
	})
	act := s.idleActivity.Get(worker)
	if act.FailureClass != agenterr.ClassAuth || !act.TurnErrored {
		t.Fatalf("after 401: class=%s errored=%v", act.FailureClass, act.TurnErrored)
	}

	s.ClearPlanAuthFailures([]string{worker})
	waitFor(t, "redrive after recovery", func() bool { return len(proc.sentSnapshot()) > 0 })
	if got := proc.sentSnapshot()[0]; got != "" {
		t.Fatalf("redrive submit=%q want empty broker handoff", got)
	}
	if n := s.pendingAgentSends(worker); n != 1 {
		t.Fatalf("pending after redrive=%d want 1 (work rides the new turn)", n)
	}

	// Provider answers ok on the redriven turn.
	proc.mu.Lock()
	proc.inFlight = false
	proc.mu.Unlock()
	sink(claudia.Event{Type: "assistant", Text: "Resuming refused work.", StopReason: "end_turn"})
	if s.idleActivity.Get(worker).RefusalHold {
		t.Fatal("substantive ok turn must clear refusal hold")
	}
}

// The default logger is process-wide: parallel tests can write while this test
// reads its captured output. Serialize both sides of that observation.
type t1213LogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *t1213LogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *t1213LogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestT959ImpatienceLadderLogsHeldRefusalGap(t *testing.T) {
	t.Parallel()
	now := time.Unix(20_000, 0)
	var buf t1213LogBuffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	s, activity := impatienceFixture(t, now)
	rep := &recordingRepressure{}
	eng := NewImpatienceEngine(ImpatienceEngineArgs{Sinks: converge.Sinks{RePressure: rep}})
	s.SetImpatienceEngine(eng)
	s.SetIdlePressureHooks(IdlePressureHooks{MissionOpen: func(string) bool { return true }})
	running := func(name string) bool { return name == "jv-t317-stuck" }

	s.idlePressureSweep(idlePressureDeps{Now: now, Running: running})
	s.idlePressureSweep(idlePressureDeps{
		Now: now.Add(converge.RepressureAfter), Running: running,
	})
	rep.agents = nil

	revoked := `provider refused the turn: 401 OAuth access token has been revoked`
	if got := activity.ApplyTerminalCause("jv-t317-stuck", agenterr.CauseAuth); got != agenterr.ClassAuth {
		t.Fatalf("auth cause class=%s", got)
	}
	activity.by["jv-t317-stuck"] = IdleActivity{
		Phase: "idle", Updated: now, RefusalHold: true, LastTerminal: revoked,
		FailureClass: agenterr.ClassAuth, TurnErrored: true,
	}

	s.idlePressureSweep(idlePressureDeps{
		Now: now.Add(converge.HumanAlertAfter + time.Hour), Running: running,
	})
	if log := buf.String(); !strings.Contains(log, "impatience ladder held open refusal gap") {
		t.Fatalf("want held-open refusal log, got:\n%s", log)
	}
}
