// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/mcpattach"
	"github.com/marcelocantos/jevons/internal/thread"
)

func TestEnsureRegisteredSetsMCPServersFromAttach(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	f := NewClaudia(reg)
	dir := t.TempDir()
	f.SetMCP(mcpattach.Args{
		Name:       "jevonsmcp",
		URL:        "http://127.0.0.1:13705/mcp",
		ClaudeJSON: filepath.Join(dir, "claude.json"),
		GrokTOML:   filepath.Join(dir, "grok.toml"),
		CodexTOML:  filepath.Join(dir, "codex.toml"),
	})
	th := &thread.Thread{
		ID:       "jv-t464-codex",
		WorkDir:  t.TempDir(),
		Provider: "codex",
		Parent:   "jevons-po",
		Purpose:  claudia.PurposeWork,
	}
	if err := f.ensureRegistered(th); err != nil {
		t.Fatal(err)
	}
	def := reg.Def(th.ID)
	if def == nil || len(def.MCPServers) != 1 || def.MCPServers[0].Name != "jevonsmcp" {
		t.Fatalf("MCPServers = %+v", def)
	}
	if def.MCPServers[0].URL != "http://127.0.0.1:13705/mcp" {
		t.Fatalf("url = %q", def.MCPServers[0].URL)
	}
	if !def.MCPExclusive {
		t.Fatal("mint must set MCPExclusive")
	}
}

func TestEnsureRegisteredBackfillsMCPExclusive(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-old", SessionID: "s1", Provider: claudia.ProviderGrok,
		WorkDir: t.TempDir(), Purpose: claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}
	f := NewClaudia(reg)
	if err := f.ensureRegistered(&thread.Thread{ID: "jv-old"}); err != nil {
		t.Fatal(err)
	}
	def := reg.Def("jv-old")
	if def == nil || !def.MCPExclusive {
		t.Fatalf("backfill Exclusive: %+v", def)
	}
}

func TestIsolateCodexMintKeepsJevonsmcp(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	f := NewClaudia(reg)
	dir := t.TempDir()
	f.SetMCP(mcpattach.Args{
		Name:       "jevonsmcp-journey",
		URL:        "http://127.0.0.1:13715/mcp",
		ClaudeJSON: filepath.Join(dir, "claude.json"),
		GrokTOML:   filepath.Join(dir, "grok.toml"),
		CodexTOML:  filepath.Join(dir, "codex.toml"),
		Isolate:    true,
	})
	th := &thread.Thread{
		ID: "jv-iso-codex", WorkDir: t.TempDir(), Provider: "codex",
		Parent: "jevons-po", Purpose: claudia.PurposeWork,
	}
	if err := f.ensureRegistered(th); err != nil {
		t.Fatal(err)
	}
	def := reg.Def(th.ID)
	if def == nil || len(def.MCPServers) != 1 || def.MCPServers[0].Name != "jevonsmcp-journey" {
		t.Fatalf("isolate Codex MCPServers = %+v; want journey HTTP jevonsmcp", def.MCPServers)
	}
}
