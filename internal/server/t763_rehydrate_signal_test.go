// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T763: /api/agents distinguishes a stopped seat that can come back from
// one whose rehydrate is known to fail — `status: stopped` alone is not
// that signal. The specimen is jevons-po on 2026-09-21: provider claude, a
// codex sandbox still on its def.
func TestT763AgentsFeedCarriesRehydrateSignal(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, d := range []claudia.AgentDef{
		{Name: "clean-seat", WorkDir: dir, SessionID: "s-clean", Provider: claudia.ProviderClaude},
		{Name: "jevons-po", WorkDir: dir, SessionID: "s-po", Provider: claudia.ProviderClaude,
			SandboxMode: "workspace-write"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	byName := map[string]agentInfo{}
	for _, a := range listFleetAgents(reg) {
		byName[a.Name] = a
	}
	if got := byName["clean-seat"]; got.Status != "stopped" || got.Rehydrate != "resumable" {
		t.Fatalf("clean-seat status=%q rehydrate=%q, want stopped/resumable", got.Status, got.Rehydrate)
	}
	po := byName["jevons-po"]
	if po.Status != "stopped" || !strings.HasPrefix(po.Rehydrate, "repairable:") ||
		!strings.Contains(po.Rehydrate, "sandbox_policy") {
		t.Fatalf("jevons-po status=%q rehydrate=%q, want stopped with a repairable sandbox_policy signal",
			po.Status, po.Rehydrate)
	}
}
