// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

func TestPlanCockpitDesiredOK(t *testing.T) {
	t.Parallel()
	p := planCockpit(cockpitObs{Registered: true, ProcAlive: true, ChatAttached: true}, 0, 8, 0)
	if p != cockpitOK {
		t.Fatalf("phase=%v want OK", p)
	}
}

func TestPlanCockpitAttachWhenProcAlive(t *testing.T) {
	t.Parallel()
	p := planCockpit(cockpitObs{Registered: true, ProcAlive: true, ChatAttached: false}, 3, 8, 0)
	if p != cockpitAttach {
		t.Fatalf("phase=%v want Attach", p)
	}
}

func TestPlanCockpitLaunchWhenDown(t *testing.T) {
	t.Parallel()
	p := planCockpit(cockpitObs{Registered: true, ProcAlive: false, ChatAttached: false}, 0, 8, 0)
	if p != cockpitLaunch {
		t.Fatalf("phase=%v want Launch", p)
	}
}

func TestPlanCockpitGiveUpWhenResumeDenied(t *testing.T) {
	t.Parallel()
	p := planCockpit(cockpitObs{Registered: true, ProcAlive: false, ResumeDenied: true}, 0, 8, 0)
	if p != cockpitGiveUp {
		t.Fatalf("phase=%v want GiveUp (must not relaunch a fail-closed Cursor store)", p)
	}
}

func TestPlanCockpitGiveUpAfterMaxAttempts(t *testing.T) {
	t.Parallel()
	p := planCockpit(cockpitObs{Registered: true, ProcAlive: false, ChatAttached: false}, 8, 8, 0)
	if p != cockpitGiveUp {
		t.Fatalf("phase=%v want GiveUp", p)
	}
	p = planCockpit(cockpitObs{Registered: true, ProcAlive: false, ChatAttached: false}, 99, 8, 0)
	if p != cockpitGiveUp {
		t.Fatalf("phase=%v want GiveUp", p)
	}
}

func TestPlanCockpitUnregisteredGiveUp(t *testing.T) {
	t.Parallel()
	p := planCockpit(cockpitObs{}, 0, 8, 0)
	if p != cockpitGiveUp {
		t.Fatalf("phase=%v want GiveUp", p)
	}
}

func TestPlanCockpitUnstickBusyWhenStalePrompt(t *testing.T) {
	t.Parallel()
	o := cockpitObs{
		Registered: true, ProcAlive: true, ChatAttached: true,
		PromptInFlight: true, SinceProgress: 2 * time.Minute,
	}
	p := planCockpit(o, 0, 8, 90*time.Second)
	if p != cockpitUnstickBusy {
		t.Fatalf("phase=%v want UnstickBusy", p)
	}
	// Fresh progress while busy → still OK (healthy long turn).
	o.SinceProgress = 5 * time.Second
	p = planCockpit(o, 0, 8, 90*time.Second)
	if p != cockpitOK {
		t.Fatalf("fresh progress phase=%v want OK", p)
	}
	// Queue backlog with no progress → unstick.
	o = cockpitObs{
		Registered: true, ProcAlive: true, ChatAttached: true,
		QueueDepth: 3, SinceProgress: 2 * time.Minute,
	}
	p = planCockpit(o, 0, 8, 90*time.Second)
	if p != cockpitUnstickBusy {
		t.Fatalf("queue backlog phase=%v want UnstickBusy", p)
	}
}

func TestClearConnectEndpoint(t *testing.T) {
	t.Parallel()
	in := claudia.AgentDef{
		Name: "jevons", SessionID: "s1",
		ConnectURL: "ws://127.0.0.1:9/ws?server-key=x", ConnectPID: 4242,
		Materialized: true,
	}
	out := clearConnectEndpoint(in)
	if out.ConnectURL != "" || out.ConnectPID != 0 {
		t.Fatalf("connect not cleared: url=%q pid=%d", out.ConnectURL, out.ConnectPID)
	}
	if out.SessionID != "s1" || !out.Materialized {
		t.Fatalf("other fields mutated: %+v", out)
	}
	if in.ConnectURL == "" {
		t.Fatal("clearConnectEndpoint must not mutate input")
	}
}

func TestRewindRotateClearsConnect(t *testing.T) {
	t.Parallel()
	// Mirrors RewindOverseer: after Stop, rotate must not re-persist
	// pre-stop ConnectURL (the bug that left agents.json pointing at a
	// killed serve and forced reattach → connection reset).
	pre := claudia.AgentDef{
		Name: "jevons", SessionID: "old",
		ConnectURL: "ws://127.0.0.1:62475/ws?server-key=dead",
		ConnectPID: 58926, Materialized: true, AutoStart: true,
	}
	rotated := clearConnectEndpoint(pre)
	rotated.SessionID = "new-uuid"
	rotated.Materialized = false
	if rotated.ConnectURL != "" || rotated.ConnectPID != 0 {
		t.Fatalf("rotated still has connect: %+v", rotated)
	}
	if rotated.SessionID == pre.SessionID || rotated.Materialized {
		t.Fatalf("rotation incomplete: %+v", rotated)
	}
}
