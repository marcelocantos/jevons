// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package upgrade

import (
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
)

func TestFromRegistryEmpty(t *testing.T) {
	if got := FromRegistry(nil); got != nil {
		t.Fatalf("nil reg: %v", got)
	}
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := FromRegistry(reg); len(got) != 0 {
		t.Fatalf("empty reg: %v", got)
	}
}

func TestFromRegistryCapturesSessionID(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name:      "jevons",
		WorkDir:   t.TempDir(),
		SessionID: "feedface-0000-0000-0000-000000000001",
		Provider:  claudia.ProviderGrok,
		AutoStart: false,
	}); err != nil {
		t.Fatal(err)
	}
	got := FromRegistry(reg)
	if len(got) != 1 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].SessionID != "feedface-0000-0000-0000-000000000001" {
		t.Fatalf("session = %q", got[0].SessionID)
	}
	if got[0].Alive {
		t.Fatal("expected not alive without Launch")
	}
	if got[0].PID != 0 {
		t.Fatal("PID must be 0 when def has no ConnectPID")
	}
}

func TestFromRegistryCapturesConnectEndpoint(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name:       "worker",
		WorkDir:    t.TempDir(),
		SessionID:  "sess-connect",
		Provider:   claudia.ProviderGrok,
		ConnectURL: "ws://127.0.0.1:9/ws?server-key=x",
		ConnectPID: 4242,
	}); err != nil {
		t.Fatal(err)
	}
	got := FromRegistry(reg)
	if len(got) != 1 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].ConnectURL == "" || got[0].PID != 4242 {
		t.Fatalf("connect endpoint not captured: %+v", got[0])
	}
	plan := PlanReattach(&Snapshot{Agents: got})
	if !plan.ProcessReattachPossible {
		t.Fatal("process reattach should be possible with connect URL+PID")
	}
	if plan.Residual != "" {
		t.Fatalf("residual = %q, want empty", plan.Residual)
	}
}

func TestPlanReattachResidual(t *testing.T) {
	plan := PlanReattach(&Snapshot{
		Agents: []Handle{{Name: "a", SessionID: "s1"}, {Name: "b", SessionID: "s2"}},
	})
	if plan.ProcessReattachPossible {
		t.Fatal("process reattach must be false without connect endpoints")
	}
	if plan.Residual == "" {
		t.Fatal("residual required when no connect endpoints")
	}
	if len(plan.SessionIDs) != 2 {
		t.Fatalf("sessions = %v", plan.SessionIDs)
	}
	if p := PlanReattach(nil); len(p.SessionIDs) != 0 || p.ProcessReattachPossible {
		t.Fatalf("nil plan = %+v", p)
	}
}

func TestPlanReattachConnectMode(t *testing.T) {
	plan := PlanReattach(&Snapshot{
		Agents: []Handle{{
			Name: "jevons", SessionID: "s1",
			PID: 100, ConnectURL: "ws://127.0.0.1:1/ws?server-key=k",
		}},
	})
	if !plan.ProcessReattachPossible {
		t.Fatal("want process reattach possible")
	}
	if plan.Residual != "" {
		t.Fatalf("residual = %q", plan.Residual)
	}
}
