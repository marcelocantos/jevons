// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"context"
	"errors"
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
	if got := CapDrop("w"); !strings.Contains(got, "codex→claude") || !strings.Contains(got, `sandbox_policy mode="workspace-write"`) {
		t.Fatalf("CapDrop=%q, want the switch and the dropped sandbox recorded", got)
	}

	// Codex keeps its sandbox: nothing it supports is dropped.
	back := *def
	back.SandboxMode = "danger-full-access"
	if err := switchProvider(&back, claudia.ProviderCodex, "test"); err != nil {
		t.Fatal(err)
	}
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
	// No provider-native setting on the def: the refusal must not be
	// blamed on a provider switch (jevons-po review of c9fe081f).
	for _, want := range []string{"image_input", "claude provider refused", "not a stored provider-switch setting"} {
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

// TestT763SwitchNeverDropsARestriction pins jevons-po's second review
// point: the launch-time and switch-time cleanup cannot silently weaken a
// security policy. A read-only codex sandbox is a restriction claude cannot
// enforce, so the switch is refused before the seat is touched, and a def
// already in that state reads broken, naming it, instead of being scrubbed.
func TestT763SwitchNeverDropsARestriction(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	var started []claudia.Config
	reg.SetLaunchers(&claudia.RegistryLaunchers{Start: t763Start(&started)})
	ro := claudia.AgentDef{Name: "ro-auditor", WorkDir: t.TempDir(), SessionID: "t763-ro",
		Provider: claudia.ProviderCodex, SandboxMode: "read-only"}
	if err := reg.Register(ro); err != nil {
		t.Fatal(err)
	}
	_, err = NewClaudia(reg).rotate("ro-auditor", claudia.ProviderClaude, true, "migrate")
	if !errors.Is(err, ErrProviderSwitchWouldWeaken) || !strings.Contains(err.Error(), `"read-only"`) {
		t.Fatalf("rotate err=%v, want a refusal naming the read-only sandbox", err)
	}
	if def := reg.Def("ro-auditor"); def.Provider != claudia.ProviderCodex || def.SandboxMode != "read-only" {
		t.Fatalf("refused switch still changed the seat: %+v", def)
	}

	// Already stale (claudia recordMigrate moved it): not scrubbed, broken.
	stale := ro
	stale.Name, stale.SessionID, stale.Provider = "ro-stale", "t763-ro-stale", claudia.ProviderClaude
	if err := reg.Register(stale); err != nil {
		t.Fatal(err)
	}
	if got := RehydrateHealth(*reg.Def("ro-stale")); !strings.HasPrefix(got, "broken:") || !strings.Contains(got, "read-only") {
		t.Fatalf("RehydrateHealth=%q, want broken naming read-only", got)
	}
	if _, err := LaunchReconciled(reg, "ro-stale"); !errors.Is(err, ErrProviderSwitchWouldWeaken) {
		t.Fatalf("rehydrate err=%v, want the restriction refused", err)
	}
	if def := reg.Def("ro-stale"); def.SandboxMode != "read-only" {
		t.Fatalf("rehydrate scrubbed a restriction: %+v", def)
	}
	if got := CapDrop("ro-stale"); got != "" {
		t.Fatalf("CapDrop=%q, want no drop recorded", got)
	}
	if len(started) != 0 {
		t.Fatalf("a process started for a refused seat: %+v", started)
	}

	// Hand-set extra_args are never dropped behind the operator's back.
	args := claudia.AgentDef{Name: "argv-seat", WorkDir: t.TempDir(), SessionID: "t763-argv",
		Provider: claudia.ProviderClaude, ExtraArgs: []string{"--permission-mode", "plan"}}
	if err := providerSwitchRefusal(args, claudia.ProviderCodex); !errors.Is(err, ErrProviderSwitchWouldWeaken) {
		t.Fatalf("claude→codex with extra_args err=%v, want refusal", err)
	}
}
