// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpattach

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T1039: LoadMCP inventory still lists a dead isolate's nested proxy,
// but Proxied carries the owner's direct grant. AgentDef.MCPServers
// must receive the direct URL, not :52322/upstream/bullseye.
func TestT1039SessionServersGrantsDirectOverStaleNested(t *testing.T) {
	a := fixtureArgs(t, "jevonsmcp", "http://127.0.0.1:13705/mcp")
	doc := map[string]any{
		"mcpServers": map[string]any{
			"bullseye":  map[string]any{"type": "http", "url": "http://127.0.0.1:52322/upstream/bullseye"},
			"atlassian": map[string]any{"type": "http", "url": "https://mcp.atlassian.com/v1/mcp/authv2"},
		},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.ClaudeJSON, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	a.Proxied = []claudia.MCPServer{
		{Name: "bullseye", Type: "http", URL: "http://127.0.0.1:18743/mcp"},
		{Name: "atlassian", Type: "http", URL: "http://127.0.0.1:13705/upstream/atlassian"},
	}
	list := SessionServers(a, claudia.ProviderClaude, "")
	byName := map[string]claudia.MCPServer{}
	for _, s := range list {
		byName[s.Name] = s
	}
	if byName["bullseye"].URL != "http://127.0.0.1:18743/mcp" {
		t.Fatalf("fresh mint bullseye = %q; want owner direct grant", byName["bullseye"].URL)
	}
	if byName["atlassian"].URL != "http://127.0.0.1:13705/upstream/atlassian" {
		t.Fatalf("atlassian = %q; remote OAuth still takes the T520 proxy", byName["atlassian"].URL)
	}
	if byName["jevonsmcp"].URL != a.URL {
		t.Fatalf("jevonsmcp = %+v", byName["jevonsmcp"])
	}
}

// Inventory already has the direct loopback URL (today's ~/.claude.json).
// applyProxied must not rewrite it to this daemon's /upstream/ leftover.
func TestT1039SessionServersKeepsDirectWhenProxiedIsNested(t *testing.T) {
	a := fixtureArgs(t, "jevonsmcp", "http://127.0.0.1:13705/mcp")
	doc := map[string]any{
		"mcpServers": map[string]any{
			"bullseye": map[string]any{"type": "http", "url": "http://127.0.0.1:18743/mcp"},
		},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.ClaudeJSON, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	a.Proxied = []claudia.MCPServer{{
		Name: "bullseye", Type: "http", URL: "http://127.0.0.1:13705/upstream/bullseye",
	}}
	list := SessionServers(a, claudia.ProviderClaude, "")
	byName := map[string]claudia.MCPServer{}
	for _, s := range list {
		byName[s.Name] = s
	}
	if byName["bullseye"].URL != "http://127.0.0.1:18743/mcp" {
		t.Fatalf("direct inventory rewritten to nested proxy: %q", byName["bullseye"].URL)
	}
}
