// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/mcpattach"
)

// 🎯T1079: the MCP servers a seat is given, and the copy the owner reads
// when the overseer is down, do not depend on which spelling its row
// stores. A grok overseer got jevonsmcp alone (the retired 🎯T525 Grok ACP
// exception) while the same seat stored as xai-oauth got the full set, and an
// anthropic overseer was told to sign in to the Grok CLI.
func TestT1079LegacyPlanRowMatchesLaunchRow(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	pairs := [][2]claudia.Provider{{"grok", "xai-oauth"}, {"claude", "anthropic"}, {"codex", "openai-codex"}}
	for _, pair := range pairs {
		for _, p := range pair {
			if err := reg.Register(claudia.AgentDef{Name: "seat-" + string(p), SessionID: "s-" + string(p), Provider: p, WorkDir: t.TempDir()}); err != nil {
				t.Fatal(err)
			}
		}
	}
	dir := t.TempDir()
	claude := filepath.Join(dir, "claude.json")
	if err := os.WriteFile(claude, []byte(`{"mcpServers":{"bullseye":{"type":"http","url":"http://127.0.0.1:18743/mcp"},"sawmill":{"command":"/nonexistent/sawmill"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	attach := mcpattach.Args{
		Name: "jevonsmcp", URL: "http://127.0.0.1:13705/mcp",
		ClaudeJSON: claude, GrokTOML: filepath.Join(dir, "grok.toml"), CodexTOML: filepath.Join(dir, "codex.toml"),
	}
	overseer := &claudia.AgentDef{Name: "jevons", SessionID: "root", Provider: "grok"}
	stampRegistrySessionMCP(reg, attach, overseer)
	want := reg.Def("seat-xai-oauth").MCPServers
	if len(want) != 3 {
		t.Fatalf("launch-id seat MCPServers = %+v, want bullseye, sawmill and jevonsmcp", want)
	}
	if !t1079SameServers(overseer.MCPServers, want) {
		t.Errorf("grok overseer MCPServers = %+v, want the xai-oauth seat's %+v", overseer.MCPServers, want)
	}
	for _, pair := range pairs {
		a, b := reg.Def("seat-"+string(pair[0])), reg.Def("seat-"+string(pair[1]))
		if !t1079SameServers(a.MCPServers, b.MCPServers) || !t1079SameServers(a.MCPServers, want) {
			t.Errorf("%s seat MCPServers = %+v, %s seat = %+v", pair[0], a.MCPServers, pair[1], b.MCPServers)
		}
		if diagnoseOverseerUnavailable(pair[0], true, "") != diagnoseOverseerUnavailable(pair[1], true, "") {
			t.Errorf("overseer-down copy differs for %s and %s", pair[0], pair[1])
		}
	}
}

// t1079SameServers compares as sets: LoadMCP reads a JSON object, whose
// order is not the seat's business.
func t1079SameServers(a, b []claudia.MCPServer) bool {
	byName := func(x []claudia.MCPServer) []claudia.MCPServer {
		out := slices.Clone(x)
		slices.SortFunc(out, func(p, q claudia.MCPServer) int { return strings.Compare(p.Name, q.Name) })
		return out
	}
	return mcpattach.ServersEqual(byName(a), byName(b))
}
