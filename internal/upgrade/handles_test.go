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

func TestPlanReattachTmuxWindow(t *testing.T) {
	plan := PlanReattach(&Snapshot{
		Agents: []Handle{{
			Name: "worker", SessionID: "s1", TmuxWindowID: "@4",
		}},
	})
	if !plan.ProcessReattachPossible {
		t.Fatal("tmux window id should make process reattach possible")
	}
	if plan.Residual != "" {
		t.Fatalf("residual = %q", plan.Residual)
	}
}

func TestConsumeSnapshot(t *testing.T) {
	path := SnapshotPath(t.TempDir())
	if err := SaveSnapshot(path, BuildSnapshot([]Handle{{Name: "a", SessionID: "s"}}, 1)); err != nil {
		t.Fatal(err)
	}
	if err := ConsumeSnapshot(path); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSnapshot(path)
	if err != nil || got != nil {
		t.Fatalf("after consume: (%v, %v)", got, err)
	}
	if err := ConsumeSnapshot(path); err != nil {
		t.Fatal(err)
	}
}

func TestProcessReattachableCursorPIDOnly(t *testing.T) {
	if processReattachable(Handle{Provider: "cursor", PID: 39004, SessionID: "s"}) {
		t.Fatal("Cursor stdio PID must not count as adoptable (🎯T541.1)")
	}
	if !processReattachable(Handle{PID: 1, ConnectURL: "ws://127.0.0.1/x"}) {
		t.Fatal("Grok URL+PID must stay adoptable")
	}
}

func TestProcessReattachableACPWindowIDIsNotTmux(t *testing.T) {
	codex := Handle{Provider: "codex", SessionID: "01a0320f", TmuxWindowID: "codex-app-server-01a0320f"}
	if processReattachable(codex) {
		t.Fatal("Codex ACP WindowID must not count as a tmux adopt (🎯T545.1.2)")
	}
	if !ShouldStopOnUpgrade(codex, true) {
		t.Fatal("live Codex seat must stop on upgrade exit so exclusive home can flush")
	}
	cursor := Handle{Provider: "cursor", SessionID: "0beb", TmuxWindowID: "cursor-acp-0beb"}
	if processReattachable(cursor) {
		t.Fatal("Cursor ACP WindowID must not count as a tmux adopt (🎯T541.1)")
	}
	if !ShouldStopOnUpgrade(cursor, true) {
		t.Fatal("live Cursor seat with ACP WindowID must still stop")
	}
	if adoptiveTmuxWindowID("codex-app-server-01a0320f") != "" {
		t.Fatal("ACP label must not be copied onto TmuxWindowID")
	}
	if adoptiveTmuxWindowID("@12") != "@12" {
		t.Fatal("Claude @N must stay a tmux adopt")
	}
	if !tmuxAdoptableWindow("%3") {
		t.Fatal("tmux pane %N must stay a tmux adopt")
	}
	if tmuxAdoptableWindow("@") || tmuxAdoptableWindow("@x") {
		t.Fatal("truncated or non-numeric @ id is not tmux")
	}
}

func TestShouldStopOnUpgrade(t *testing.T) {
	cursor := Handle{Provider: "cursor", SessionID: "0beb", PID: 27606}
	if !ShouldStopOnUpgrade(cursor, true) {
		t.Fatal("live Cursor seat must stop on upgrade exit")
	}
	if !ShouldStopOnUpgrade(cursor, false) {
		t.Fatal("Cursor leftover with session must stop/reap on upgrade exit")
	}
	grok := Handle{Provider: "grok", SessionID: "g", PID: 9, ConnectURL: "ws://127.0.0.1/x"}
	if ShouldStopOnUpgrade(grok, true) {
		t.Fatal("Grok connect-mode must survive upgrade exit")
	}
	claude := Handle{Provider: "", SessionID: "c", TmuxWindowID: "@3"}
	if ShouldStopOnUpgrade(claude, true) {
		t.Fatal("Claude tmux must survive upgrade exit")
	}
}

func TestStopNonAdoptableStopsCursorLeavesGrok(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons", WorkDir: t.TempDir(), SessionID: "sid-cursor",
		Provider: claudia.ProviderCursor, ConnectPID: 42424242, AutoStart: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "worker", WorkDir: t.TempDir(), SessionID: "sid-grok",
		Provider: claudia.ProviderGrok, ConnectURL: "ws://127.0.0.1:9/ws",
		ConnectPID: 4242, AutoStart: true,
	}); err != nil {
		t.Fatal(err)
	}
	n := StopNonAdoptable(reg)
	if n != 1 {
		t.Fatalf("stopped %d, want 1 (cursor only)", n)
	}
	if d := reg.Def("jevons"); d != nil && d.ConnectPID != 0 {
		t.Fatalf("cursor ConnectPID still %d after stop", d.ConnectPID)
	}
	if d := reg.Def("worker"); d == nil || d.ConnectPID != 4242 || d.ConnectURL == "" {
		t.Fatal("grok connect endpoint must survive")
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
