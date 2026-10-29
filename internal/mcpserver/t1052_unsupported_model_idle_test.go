// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/turnev"
)

// jv-t1046-failed-row-probe native refusal (deterministic unsupported-model 400).
const t1052NativeRefusal = `The '` + cost.ModelCodexSpark + `' model is not supported when using Codex with a ChatGPT account.`

// 🎯T1052 hermetic: unsupported pin blocks idle-nudge/repressure without erasing history.
func TestT1052UnsupportedModelPinBlocksIdleRepressure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	intentPath := filepath.Join(dir, "intent")
	st, err := fleetintent.Open(intentPath)
	if err != nil {
		t.Fatal(err)
	}
	const name = "jv-t1046-failed-row-probe"
	const session = "0199c0de-session"
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: session, Provider: claudia.ProviderCodex,
		Model: cost.ModelCodexSpark, Purpose: claudia.PurposeWork, Parent: "jevons-po",
		TargetID: "T1052", Materialized: true,
	}); err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.idleActivity = NewIdleActivityTracker()
	s.SetFleetIntentStore(st)

	s.agentEventSink(name)(claudia.Event{Type: "system", IsError: true, Text: t1052NativeRefusal})

	if got := st.Snapshot().AgentState(name); got != fleetintent.BlockedProvider {
		t.Fatalf("intent=%s want blocked_provider", got)
	}
	if got := st.Snapshot().FleetState(); got != fleetintent.Working {
		t.Fatalf("fleet blocked: %s", got)
	}
	_, _, failed := s.ProviderRequestFailure(name)
	if !failed {
		t.Fatal("provider request not latched failed")
	}
	row := reg.Def(name)
	if row == nil || row.SessionID != session || row.Model != cost.ModelCodexSpark {
		t.Fatalf("history row changed: %+v", row)
	}
	for _, c := range []fleetintent.Control{fleetintent.ControlNudge, fleetintent.ControlRepressure, fleetintent.ControlRevive, fleetintent.ControlRepair} {
		if s.AllowFleetControl(name, c).Allow {
			t.Fatalf("seat allowed %s while blocked", c)
		}
	}

	now := time.Unix(1_900_000_000, 0).UTC()
	activity := s.idleActivity
	activity.mu.Lock()
	activity.by[name] = IdleActivity{
		Phase:                 "idle",
		Updated:               now.Add(-DefaultIdleNudgeThreshold - 12*time.Minute),
		ProviderRequestFailed: true,
	}
	activity.mu.Unlock()

	var pushes atomic.Int32
	runSweep := func(at time.Time) {
		SweepIdleNudges(IdleNudgeSweepArgs{
			Reg:            reg,
			Activity:       activity,
			Intent:         st.Snapshot(),
			Now:            at,
			OverseerName:   "jevons",
			SessionPhase:   func(claudia.AgentDef) turnev.Phase { return turnev.PhaseIdle },
			ProcessRunning: func(string) bool { return true },
			Push: func(target, event, text string) error {
				if target == name {
					pushes.Add(1)
				}
				return nil
			},
		})
	}
	runSweep(now)
	runSweep(now.Add(12 * time.Minute))
	if pushes.Load() != 0 {
		t.Fatalf("idle nudge pushed %d provider turns", pushes.Load())
	}
	obs := FleetRecoverObs{
		Name:                  name,
		Purpose:               claudia.PurposeWork,
		ProcessRunning:        true,
		HasOpenMission:        true,
		NeedsRecover:          true,
		TerminalEmpty:         true,
		ProviderRequestFailed: true,
	}
	if act, _ := ClassifyFleetRecover(obs); act != FleetRecoverSkip {
		t.Fatalf("fleet recover %s", act)
	}
	if act, reason := ClassifyIdleNudge(IdleNudgeObs{
		Name: name, Purpose: claudia.PurposeWork,
		FleetIntent: fleetintent.Working, Intent: fleetintent.BlockedProvider,
		ProcessRunning: true, Phase: "idle", IdleFor: 2 * time.Hour, HasOpenMission: true,
		ProviderRequestFailed: true,
	}); act != IdleNudgeSkip || reason == "" {
		t.Fatalf("classifier %s/%s", act, reason)
	}

	if err := s.SetAgentIntent(name, fleetintent.Working, "jevons-po", "owner approved model correction"); err != nil {
		t.Fatal(err)
	}
	if !s.AllowFleetControl(name, fleetintent.ControlNudge).Allow {
		t.Fatal("explicit owner clearance did not restore nudge")
	}
	_, _, failed = s.ProviderRequestFailure(name)
	if failed {
		t.Fatal("provider failure latch survived owner clearance")
	}
	if act, reason := ClassifyIdleNudge(IdleNudgeObs{
		Name: name, Purpose: claudia.PurposeWork,
		FleetIntent: fleetintent.Working, Intent: fleetintent.Working,
		ProcessRunning: true, Phase: "idle", IdleFor: 2 * time.Hour, HasOpenMission: true,
	}); act == IdleNudgeSkip && reason == IdleSkipProviderRequestFailed {
		t.Fatalf("idle still blocked after clearance: %s", reason)
	}
}
