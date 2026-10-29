// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleet"
)

// t1032Launch enforces the same sandbox_policy refusal claude applies at
// launch, so a stale codex sandbox on a claude row fails closed.
func t1032Launch(started *[]claudia.Config) func(context.Context, claudia.Config) (*claudia.Agent, error) {
	return func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
		*started = append(*started, cfg)
		wantsSandbox := cfg.SandboxMode != "" || len(cfg.SandboxWritableRoots) > 0 || cfg.SandboxNetworkAccess || cfg.SandboxGitWrite
		if wantsSandbox {
			if err := claudia.CheckCapability(cfg.Provider, claudia.CapabilitySandboxPolicy); err != nil {
				return nil, err
			}
		}
		return claudia.StartStub(ctx, cfg, nil)
	}
}

// TestT1032AgentStartCrossProviderDropsCodexSandbox is the acceptance
// repro: a stopped codex seat with workspace-write, then jevons_agent_start
// with provider=claude, must register and launch without sandbox_policy
// refusal.
func TestT1032AgentStartCrossProviderDropsCodexSandbox(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	workdir := t.TempDir()
	const name = "jv-t1032-cross"
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: workdir, SessionID: "t1032-codex",
		Provider: claudia.ProviderCodex, Purpose: claudia.PurposeWork,
		SandboxMode: "workspace-write", SandboxWritableRoots: []string{workdir},
		SandboxNetworkAccess: true, SandboxGitWrite: true,
	}); err != nil {
		t.Fatal(err)
	}
	reg.Stop(name)

	var started []claudia.Config
	reg.SetLaunchers(&claudia.RegistryLaunchers{Start: t1032Launch(&started)})

	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)

	def, existed, _, err := s.stitchAgentStart(
		name, workdir, "", "claude", "", "jevons-po", claudia.PurposeWork, "", "",
	)
	if err != nil {
		t.Fatalf("stitchAgentStart codex→claude: %v", err)
	}
	if !existed {
		t.Fatal("expected resume of existing row")
	}
	if len(fleet.StaleProviderCaps(*def)) != 0 || def.SandboxMode != "" || def.SandboxGitWrite {
		t.Fatalf("row still carries codex sandbox: %+v", def)
	}
	if got := fleet.CapDrop(name); !strings.Contains(got, "(start)") || !strings.Contains(got, "sandbox_policy") {
		t.Fatalf("CapDrop=%q, want recorded start drop", got)
	}

	if _, err := reg.Launch(name); err != nil {
		if strings.Contains(err.Error(), "sandbox_policy capability is unsupported") {
			t.Fatalf("launch refused stale sandbox after stitch: %v", err)
		}
		t.Fatalf("launch: %v", err)
	}
	if len(started) == 0 || started[len(started)-1].Provider != "anthropic" {
		t.Fatalf("did not launch claude: %+v", started)
	}
}

func TestT1032AgentStartRefusesRestrictiveCodexSandbox(t *testing.T) {
	for _, mode := range []string{"read-only", "unknown-policy"} {
		t.Run(mode, func(t *testing.T) {
			reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
			if err != nil {
				t.Fatal(err)
			}
			workdir := t.TempDir()
			const name = "jv-t1032-restricted"
			src := claudia.AgentDef{
				Name: name, WorkDir: workdir, SessionID: "t1032-ro",
				Provider: claudia.ProviderCodex, Purpose: claudia.PurposeWork,
				SandboxMode: mode,
			}
			if err := reg.Register(src); err != nil {
				t.Fatal(err)
			}
			reg.Stop(name)

			s := New(t.TempDir(), nil, nil)
			s.SetRegistry(reg)
			_, _, _, err = s.stitchAgentStart(
				name, workdir, "", "claude", "", "jevons-po", claudia.PurposeWork, "", "",
			)
			if !errors.Is(err, fleet.ErrProviderSwitchWouldWeaken) {
				t.Fatalf("err=%v, want refusal to drop restriction", err)
			}
			if got := reg.Def(name); got.SandboxMode != mode || got.Provider != claudia.ProviderCodex {
				t.Fatalf("refusal changed source row: %+v", got)
			}
		})
	}
}
