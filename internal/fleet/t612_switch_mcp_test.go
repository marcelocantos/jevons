// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/mcpattach"
)

// 🎯T612: overseer migrate/switch onto Grok must restamp MCPServers from
// the live attach list so a Claude-era owner-map inventory does not survive
// on the registry row the sidecar reads.
func TestT612SwitchRestampsMCPServersAndClearsGrokConnect(t *testing.T) {
	t.Setenv("CLAUDIA_NO_BROKER", "1")
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	dir := t.TempDir()
	claude := filepath.Join(dir, "claude.json")
	if err := os.WriteFile(claude, []byte(`{"mcpServers":{"bullseye":{"type":"http","url":"http://127.0.0.1:18743/mcp"},"playwright":{"command":"npx","args":["@playwright/mcp"]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	f := NewClaudia(reg)
	f.SetMCP(mcpattach.Args{
		Name: "jevonsmcp", URL: "http://127.0.0.1:13705/mcp",
		ClaudeJSON: claude, GrokTOML: filepath.Join(dir, "grok.toml"), CodexTOML: filepath.Join(dir, "codex.toml"),
	})
	stale := []claudia.MCPServer{
		{Name: "bullseye", Type: "http", URL: "http://127.0.0.1:9/mcp"},
		{Name: "playwright", Command: "npx", Args: []string{"@playwright/mcp", "--user-data-dir", "/tmp/shared"}},
		{Name: "jevonsmcp", Type: "http", URL: "http://127.0.0.1:1/mcp"},
		{Name: "mnemo", Type: "http", URL: "http://127.0.0.1:13705/upstream/mnemo"},
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons", SessionID: "root", Provider: "anthropic", WorkDir: work,
		MCPServers: stale, MCPExclusive: true, GrokConnect: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.restampSeatMCPServers("jevons"); err != nil {
		t.Fatal(err)
	}
	def := reg.Def("jevons")
	if def == nil {
		t.Fatal("missing overseer row")
	}
	if def.GrokConnect {
		t.Fatal("exclusive switch restamp must clear grok_connect leftovers")
	}
	want := f.SessionMCPServers(work)
	if mcpServersEqual(def.MCPServers, stale) {
		t.Fatalf("MCPServers still stale: %+v", def.MCPServers)
	}
	if len(def.MCPServers) != len(want) {
		t.Fatalf("MCPServers = %+v, want %d servers like %+v", def.MCPServers, len(want), want)
	}
	hasJevons := false
	for _, s := range def.MCPServers {
		if s.Name == "jevonsmcp" && s.URL == "http://127.0.0.1:13705/mcp" {
			hasJevons = true
		}
	}
	if !hasJevons {
		t.Fatalf("restamp must carry live jevonsmcp: %+v", def.MCPServers)
	}
}
