// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/mcpattach"
)

func TestT464StitchMintCarriesJevonsmcp(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	dir := t.TempDir()
	s.SetMCP(mcpattach.Args{
		Name:       "jevonsmcp",
		URL:        "http://127.0.0.1:13705/mcp",
		ClaudeJSON: filepath.Join(dir, "claude.json"),
		GrokTOML:   filepath.Join(dir, "grok.toml"),
		CodexTOML:  filepath.Join(dir, "codex.toml"),
	})
	def, _, _, err := s.stitchAgentStart(
		"jv-t464-worker", t.TempDir(), "", string(claudia.ProviderCodex), "",
		"jevons-po", claudia.PurposeWork, "T464", "use jevons tools",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(def.MCPServers) != 1 || def.MCPServers[0].Name != "jevonsmcp" {
		t.Fatalf("MCPServers = %+v", def.MCPServers)
	}
	cfg := startConfigFromDef(def)
	if len(cfg.MCPServers) != 1 {
		t.Fatalf("launch Config.MCPServers = %+v", cfg.MCPServers)
	}
}
