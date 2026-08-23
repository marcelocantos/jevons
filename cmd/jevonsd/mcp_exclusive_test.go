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
	if len(overseer.MCPServers) != 1 || overseer.MCPServers[0].Name != "jevonsmcp" {
		t.Fatalf("overseer MCPServers = %+v", overseer.MCPServers)
	}
	po := reg.Def("jevons-po")
	if po == nil {
		t.Fatal("missing PO")
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

func TestStampRegistryMCPExclusiveOverseerAndWorkers(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons-po", SessionID: "po", Provider: claudia.ProviderClaude,
	}); err != nil {
		t.Fatal(err)
	}
	overseer := &claudia.AgentDef{
		Name: "jevons", SessionID: "root", Provider: claudia.ProviderGrok,
		ConnectURL: "ws://127.0.0.1:9/ws", ConnectPID: 999999,
	}
	stampRegistryMCPExclusive(reg, overseer)
	if !overseer.MCPExclusive {
		t.Fatal("overseer Exclusive")
	}
	if overseer.ConnectURL != "" || overseer.ConnectPID != 0 {
		t.Fatalf("overseer leftover connect survived: %+v", overseer)
	}
	po := reg.Def("jevons-po")
	if po == nil || !po.MCPExclusive {
		t.Fatalf("PO Exclusive: %+v", po)
	}
}
