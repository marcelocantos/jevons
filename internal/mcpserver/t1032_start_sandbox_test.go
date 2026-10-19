// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleet"
)

// t1032Start stands in for claudia's provider launch: a provider that
// refuses sandbox_policy refuses a Config that requests it — the exact
// CapabilityError the 2026-10-07 incident saw on claude.
func t1032Start(started *[]claudia.Config) func(context.Context, claudia.Config) (*claudia.Agent, error) {
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

// TestT1032AgentStartDropsCodexSandboxOnClaude is the acceptance hermetic
// for 🎯T1032: a stopped codex seat carrying workspace-write sandbox_policy
// is started again with provider=claude. Pre-fix, Launch failed closed with
// "sandbox_policy capability is unsupported". Post-fix the start path
// scrubs the droppable fields, Launch succeeds, and the seat runs on claude.
func TestT1032AgentStartDropsCodexSandboxOnClaude(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	var started []claudia.Config
	reg.SetLaunchers(&claudia.RegistryLaunchers{Start: t1032Start(&started)})

	const name = "jv-t1032-codex-origin"
	wd := t.TempDir()
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: wd, SessionID: "t1032-codex-sid",
		Provider: "openai-codex", Purpose: claudia.PurposeWork,
		SandboxMode: "workspace-write", SandboxWritableRoots: []string{t.TempDir()},
		SandboxNetworkAccess: true, SandboxGitWrite: true,
	}); err != nil {
		t.Fatal(err)
	}

	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.SetDefaultProvider(string(claudia.ProviderClaude))

	def, existed, _, err := s.stitchAgentStart(
		name, wd, "", "claude", "", "jevons-po", claudia.PurposeWork, "T1032", "",
	)
	if err != nil {
		t.Fatalf("stitchAgentStart codex→claude: %v", err)
	}
	if !existed {
		t.Fatal("existing codex seat reported as fresh mint")
	}
	if cliPlan := string(def.Provider); cliPlan != "anthropic" && cliPlan != "claude" {
		// Sidecar launch provider for claude is anthropic.
		if def.Provider != "anthropic" {
			t.Fatalf("provider=%q want anthropic (claude sidecar)", def.Provider)
		}
	}
	if def.SandboxMode != "" || len(def.SandboxWritableRoots) > 0 || def.SandboxNetworkAccess || def.SandboxGitWrite {
		t.Fatalf("stitched def still carries codex sandbox: %+v", def)
	}
	if got := fleet.CapDrop(name); !strings.Contains(got, "(start)") || !strings.Contains(got, "sandbox_policy") {
		t.Fatalf("CapDrop = %q, want a start-path sandbox_policy drop", got)
	}

	if _, err := s.launchAgent(context.Background(), name); err != nil {
		if strings.Contains(err.Error(), "sandbox_policy capability is unsupported") {
			t.Fatalf("launch after cross-provider start still refused the stale sandbox: %v", err)
		}
		t.Fatalf("launch after cross-provider start: %v", err)
	}
	if n := len(started); n == 0 || started[n-1].Provider != "anthropic" {
		t.Fatalf("launch did not start a claude process: %+v", started)
	}
	if started[len(started)-1].SandboxMode != "" {
		t.Fatalf("launch Config still requested sandbox: %+v", started[len(started)-1])
	}
}

// TestT1032AgentStartRefusesReadOnlySandboxRestriction pins that a
// restriction the destination cannot enforce still fails closed without
// rewriting the row (composes with 🎯T763).
func TestT1032AgentStartRefusesReadOnlySandboxRestriction(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	const name = "jv-t1032-readonly"
	wd := t.TempDir()
	src := claudia.AgentDef{
		Name: name, WorkDir: wd, SessionID: "t1032-ro-sid",
		Provider: "openai-codex", Purpose: claudia.PurposeWork,
		SandboxMode: "read-only",
	}
	if err := reg.Register(src); err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.SetDefaultProvider(string(claudia.ProviderClaude))

	_, _, _, err = s.stitchAgentStart(
		name, wd, "", "claude", "", "jevons-po", claudia.PurposeWork, "", "",
	)
	if err == nil {
		t.Fatal("stitch codex read-only → claude succeeded; want a restriction refusal")
	}
	if !strings.Contains(err.Error(), "restriction") && !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("err = %v, want a restriction refusal naming read-only", err)
	}
	got := reg.Def(name)
	if got.Provider != src.Provider || got.SandboxMode != src.SandboxMode {
		t.Fatalf("refused start rewrote the row: %+v", got)
	}
}
