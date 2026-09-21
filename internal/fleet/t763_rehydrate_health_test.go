// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

// TestT763SwitchDropsAtSwitchTime pins the second acceptance clause: the
// refused capability is dropped when the provider changes, not deferred to
// the next rehydrate.
func TestT763SwitchDropsAtSwitchTime(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "w", WorkDir: t.TempDir(), SessionID: "t763-w", Provider: claudia.ProviderCodex,
		SandboxMode: "workspace-write",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewClaudia(reg).rotate("w", claudia.ProviderClaude, true, "migrate"); err != nil {
		t.Fatal(err)
	}
	def := reg.Def("w")
	if def.Provider != claudia.ProviderClaude || def.SandboxMode != "" {
		t.Fatalf("after switch, def=%+v; want claude with no sandbox", def)
	}
	if got := RehydrateHealth(*def); got != "resumable" {
		t.Fatalf("RehydrateHealth=%q, want resumable", got)
	}

	// Codex keeps its sandbox: nothing it supports is dropped.
	back := *def
	back.SandboxMode = "danger-full-access"
	switchProvider(&back, claudia.ProviderCodex, "test")
	if back.SandboxMode != "danger-full-access" {
		t.Fatalf("switch to codex dropped a sandbox codex supports: %+v", back)
	}
}

// TestT763StaleDefIsRepairedAndReported covers a def switched by a road that
// does not drop (claudia's recordMigrate, or a pre-fix mint): /api/agents
// calls it repairable, and the next rehydrate drops the setting and starts.
func TestT763StaleDefIsRepairedAndReported(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	var started []claudia.Config
	reg.SetLaunchers(&claudia.RegistryLaunchers{Start: t763Start(&started)})
	stale := claudia.AgentDef{
		Name: "stale-seat", WorkDir: t.TempDir(), SessionID: "t763-stale", Provider: claudia.ProviderClaude,
		SandboxMode: "workspace-write",
	}
	if err := reg.Register(stale); err != nil {
		t.Fatal(err)
	}
	if got := RehydrateHealth(*reg.Def("stale-seat")); !strings.HasPrefix(got, "repairable:") ||
		!strings.Contains(got, "sandbox_policy") || !strings.Contains(got, "claude") {
		t.Fatalf("RehydrateHealth=%q, want repairable naming sandbox_policy and claude", got)
	}
	if _, err := LaunchRecovering(reg, "stale-seat"); err != nil {
		t.Fatalf("rehydrate of a stale def: %v", err)
	}
	if got := RehydrateHealth(*reg.Def("stale-seat")); got != "resumable" {
		t.Fatalf("after repair RehydrateHealth=%q, want resumable", got)
	}
}

// TestT763CapabilityRefusalNamesTheSwitch pins the first acceptance clause's
// failure branch and the /api/agents broken signal: a launch refused over a
// capability names the provider and stale setting, and the seat reads as
// broken until a rehydrate succeeds.
func TestT763CapabilityRefusalNamesTheSwitch(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	refuse := true
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			if refuse {
				return nil, &claudia.CapabilityError{Provider: claudia.ProviderClaude,
					Capability: claudia.CapabilityImageInput, Status: claudia.CapabilityUnsupported}
			}
			return claudia.StartStub(ctx, cfg, nil)
		},
	})
	if err := reg.Register(claudia.AgentDef{Name: "refused", WorkDir: t.TempDir(), SessionID: "t763-refused",
		Provider: claudia.ProviderClaude}); err != nil {
		t.Fatal(err)
	}
	_, err = LaunchReconciled(reg, "refused")
	if err == nil {
		t.Fatal("launch succeeded; want the capability refusal")
	}
	for _, want := range []string{"provider switch", "image_input", "claude provider refuses"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("err=%q, want it to name %q", err, want)
		}
	}
	if got := RehydrateHealth(*reg.Def("refused")); !strings.HasPrefix(got, "broken:") {
		t.Fatalf("RehydrateHealth=%q, want broken after a failed rehydrate", got)
	}
	refuse = false
	if _, err := LaunchReconciled(reg, "refused"); err != nil {
		t.Fatal(err)
	}
	if got := RehydrateHealth(*reg.Def("refused")); got != "resumable" {
		t.Fatalf("after a good rehydrate RehydrateHealth=%q, want resumable", got)
	}
}
