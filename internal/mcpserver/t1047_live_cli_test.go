// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package mcpserver

import (
	"context"
	"github.com/marcelocantos/claudia"
	"path/filepath"
	"testing"
)

func TestT1047ExistingExplicitClaudeKeepsLiveCLITransport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	reg, err := claudia.NewRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	reg.SetLaunchers(&claudia.RegistryLaunchers{Start: func(_ context.Context, cfg claudia.Config) (*claudia.Agent, error) {
		if cfg.Provider != claudia.ProviderClaude {
			t.Fatalf("original Launch config=%q", cfg.Provider)
		}
		return claudia.NewStubAgent(nil), nil
	}})
	wd := t.TempDir()
	name := "legacy-cli"
	if err := reg.Register(claudia.AgentDef{Name: name, WorkDir: wd, SessionID: "legacy-sid", Provider: claudia.ProviderClaude}); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Launch(name); err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	def, existed, _, err := s.stitchAgentStart(name, wd, "", "claude", "", "jevons-po", claudia.PurposeWork, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !existed {
		t.Fatal("existing seat reported fresh")
	}
	if def.Provider != claudia.ProviderClaude {
		t.Fatalf("live CLI relabelled: %q", def.Provider)
	}
	disk, err := claudia.NewRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := disk.Def(name); got.Provider != claudia.ProviderClaude {
		t.Fatalf("persisted provider=%q", got.Provider)
	}
	if got := startConfigFromDef(disk.Def(name)).Provider; got != claudia.ProviderClaude {
		t.Fatalf("config=%q", got)
	}
	if _, err := reg.Launch(name); err != nil {
		t.Fatal(err)
	} // Claudia returns the same live handle.
	if _, _, _, err := s.stitchAgentStart(name, wd, "", "codex", "", "jevons-po", claudia.PurposeWork, "", ""); err == nil {
		t.Fatal("cross-plan explicit pin silently relabelled a live CLI")
	}
	if got := reg.Def(name).Provider; got != claudia.ProviderClaude {
		t.Fatalf("rejected switch mutated row to %q", got)
	}
}
