// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/mcpattach"
)

func TestStampRegistrySessionMCPAppliesProxied(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons-po", SessionID: "po", Provider: claudia.ProviderClaude,
		WorkDir: t.TempDir(),
		MCPServers: []claudia.MCPServer{
			{Name: "atlassian", Type: "http", URL: "https://mcp.atlassian.com/v1/mcp"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	claude := filepath.Join(dir, "claude.json")
	if err := os.WriteFile(claude, []byte(`{"mcpServers":{"atlassian":{"type":"http","url":"https://mcp.atlassian.com/v1/mcp"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	attach := mcpattach.Args{
		Name:       "jevonsmcp",
		URL:        "http://127.0.0.1:13705/mcp",
		ClaudeJSON: claude,
		GrokTOML:   filepath.Join(dir, "grok.toml"),
		CodexTOML:  filepath.Join(dir, "codex.toml"),
		Proxied: []claudia.MCPServer{{
			Name: "atlassian", Type: "http", URL: "http://127.0.0.1:13705/upstream/atlassian",
		}},
	}
	overseer := &claudia.AgentDef{
		Name: "jevons", SessionID: "root", Provider: claudia.ProviderGrok,
	}
	stampRegistrySessionMCP(reg, attach, overseer)
	po := reg.Def("jevons-po")
	if po == nil {
		t.Fatal("missing PO")
	}
	// 🎯T1079: the overseer (here a legacy grok row) gets the PO's list; the
	// Grok ACP exception (🎯T525) died with the Grok CLI transport.
	if !mcpattach.ServersEqual(overseer.MCPServers, po.MCPServers) {
		t.Fatalf("overseer MCPServers = %+v, want the PO's %+v", overseer.MCPServers, po.MCPServers)
	}
	byName := map[string]claudia.MCPServer{}
	for _, s := range po.MCPServers {
		byName[s.Name] = s
	}
	if byName["jevonsmcp"].URL != attach.URL {
		t.Fatalf("PO jevonsmcp = %+v", byName["jevonsmcp"])
	}
	if byName["atlassian"].URL != "http://127.0.0.1:13705/upstream/atlassian" {
		t.Fatalf("PO atlassian = %+v", byName["atlassian"])
	}
}
