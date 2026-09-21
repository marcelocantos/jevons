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

// t763Start stands in for claudia's provider launch, keeping the one check
// this target is about: a provider that refuses sandbox_policy refuses a
// Config that requests it (claudeSessionPrecheck and friends), with
// claudia's own CapabilityError from its own capability table.
func t763Start(started *[]claudia.Config) func(context.Context, claudia.Config) (*claudia.Agent, error) {
	return func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
		*started = append(*started, cfg)
		wantsSandbox := cfg.SandboxMode != "" || len(cfg.SandboxWritableRoots) > 0 || cfg.SandboxNetworkAccess
		if wantsSandbox {
			if err := claudia.CheckCapability(cfg.Provider, claudia.CapabilitySandboxPolicy); err != nil {
				return nil, err
			}
		}
		return claudia.StartStub(ctx, cfg, nil)
	}
}

// TestT763ProviderSwitchKeepsSeatRevivable is the acceptance hermetic:
// mint a seat carrying the codex-only sandbox_policy, switch it to claude,
// stop it, rehydrate — it must not fail with "sandbox_policy capability is
// unsupported".
func TestT763ProviderSwitchKeepsSeatRevivable(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	var started []claudia.Config
	reg.SetLaunchers(&claudia.RegistryLaunchers{Start: t763Start(&started)})

	const name = "jevons-po"
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: t.TempDir(), SessionID: "t763-codex-sid", Provider: claudia.ProviderCodex,
		SandboxMode: "workspace-write", SandboxWritableRoots: []string{t.TempDir()},
		SandboxNetworkAccess: true, Purpose: claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}

	f := NewClaudia(reg)
	if _, err := f.rotate(name, claudia.ProviderClaude, true, "migrate"); err != nil {
		t.Fatalf("switch codex→claude: %v", err)
	}
	reg.Stop(name)

	if _, err := LaunchRecovering(reg, name); err != nil {
		if strings.Contains(err.Error(), "sandbox_policy capability is unsupported") {
			t.Fatalf("rehydrate after provider switch refused the stale codex sandbox: %v", err)
		}
		t.Fatalf("rehydrate after provider switch: %v", err)
	}
	if n := len(started); n == 0 || started[n-1].Provider != claudia.ProviderClaude {
		t.Fatalf("rehydrate did not start a claude process: %+v", started)
	}
	def := reg.Def(name)
	if def.SandboxMode != "" || len(def.SandboxWritableRoots) > 0 || def.SandboxNetworkAccess {
		t.Fatalf("stored def still carries codex sandbox after the switch: %+v", def)
	}
}
