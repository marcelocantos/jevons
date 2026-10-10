// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package mcpserver

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/fleetintent"
)

const t1052Native = `The 'gpt-5.3-codex-spark' model is not supported when using Codex with a ChatGPT account.`
const t1052Wire = `400 invalid_request_error: ` + t1052Native

func TestT1052NativeErrorBlocksOnlySeatAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "intent")
	st, err := fleetintent.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	s.SetFleetIntentStore(st)
	// claudia's Codex turn/failed maps to a system IsError event (no
	// assistant terminal event). Exercise the actual worker event sink.
	s.agentEventSink("bad")(claudia.Event{Type: "system", IsError: true, Text: t1052Native})
	if got := s.fleetIntent().AgentState("bad"); got != fleetintent.BlockedProvider {
		t.Fatalf("native error not wired: %s", got)
	}
	if got := s.fleetIntent().FleetState(); got != fleetintent.Working {
		t.Fatalf("fleet blocked: %s", got)
	}
	for _, control := range []fleetintent.Control{fleetintent.ControlNudge, fleetintent.ControlRevive, fleetintent.ControlRepressure, fleetintent.ControlRepair} {
		if s.AllowFleetControl("bad", control).Allow {
			t.Errorf("bad seat allowed %s", control)
		}
		if !s.AllowFleetControl("good", control).Allow {
			t.Errorf("good seat refused %s", control)
		}
	}
	reopened, err := fleetintent.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.SetFleetIntentStore(reopened)
	if s.AllowFleetControl("bad", fleetintent.ControlRevive).Allow {
		t.Fatal("restart lifted block")
	}
	obs := FleetRecoverObs{Intent: reopened.Snapshot().AgentState("bad"), ProcessRunning: true, HasOpenMission: true, NeedsRecover: true, TerminalEmpty: true, FallbackModel: "gpt-5", FailureClass: agenterr.ClassRateLimit, RateLimitStrikes: 9}
	if action, _ := ClassifyFleetRecover(obs); action != FleetRecoverSkip {
		t.Fatalf("recover/fallback %s", action)
	}
	if action, _ := ClassifyIdleNudge(IdleNudgeObs{FleetIntent: fleetintent.Working, Intent: obs.Intent, ProcessRunning: true, HasOpenMission: true, Phase: "idle", IdleFor: time.Hour}); action != IdleNudgeSkip {
		t.Fatalf("nudge %s", action)
	}
	s.ObserveProviderOK()
	if s.AllowFleetControl("bad", fleetintent.ControlNudge).Allow {
		t.Fatal("provider success cleared seat")
	}
	// Only explicit operator intent clears; no model rotation was performed.
	if err := s.SetAgentIntent("bad", fleetintent.Working, "jevons-po", "owner approved model correction"); err != nil {
		t.Fatal(err)
	}
	if !s.AllowFleetControl("bad", fleetintent.ControlNudge).Allow {
		t.Fatal("explicit clearance failed")
	}
}

func TestT1052NarrowRefusal(t *testing.T) {
	for _, raw := range []string{t1052Wire, t1052Native} {
		if c := agenterr.ClassifyText(raw); c != agenterr.ClassUnsupportedModel {
			t.Errorf("%q: %s", raw, c)
		}
		if agenterr.HardBlock(agenterr.ClassUnsupportedModel, raw) {
			t.Fatal("fleet-wide hard block")
		}
	}
	for _, raw := range []string{"400 invalid_request_error: messages malformed", "model not supported", "The model is not supported on this account", "429 quota exceeded", "401 unauthorized", "500 Internal error"} {
		if agenterr.IsUnsupportedModel(raw) {
			t.Errorf("over-broad match %q", raw)
		}
	}
}
