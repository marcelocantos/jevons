// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpattach

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
)

// TestT525GrokSessionServersAreHostPassed is the iterate oracle for
// 🎯T525. Jevons LoadMCPs the inventory and passes it on the Session.
// Grok must not also load ~/.grok/config.toml. The list includes
// discovered Grok servers plus this daemon's live jevonsmcp URL.
func TestT525GrokSessionServersAreHostPassed(t *testing.T) {
	dir := t.TempDir()
	claude := filepath.Join(dir, "claude.json")
	grok := filepath.Join(dir, "grok.toml")
	codex := filepath.Join(dir, "codex.toml")
	doc := map[string]any{
		"mcpServers": map[string]any{
			"bullseye":  map[string]any{"command": "/opt/homebrew/bin/mcpbridge"},
			"jevonsmcp": map[string]any{"type": "http", "url": "http://127.0.0.1:54558/mcp"},
		},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claude, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(grok, []byte(`
[mcp_servers.mnemo]
url = "http://127.0.0.1:7700/mcp"
enabled = true

[mcp_servers.jevonsmcp]
url = "http://127.0.0.1:54558/mcp"
enabled = true
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codex, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}

	a := Args{
		Name:       "jevonsmcp",
		URL:        "http://127.0.0.1:13705/mcp",
		ClaudeJSON: claude,
		GrokTOML:   grok,
		CodexTOML:  codex,
	}
	list := SessionServers(a, claudia.ProviderGrok, "")
	byName := map[string]claudia.MCPServer{}
	for _, s := range list {
		byName[s.Name] = s
	}
	if byName["jevonsmcp"].URL != a.URL {
		t.Fatalf("jevonsmcp = %+v; want live URL %s", byName["jevonsmcp"], a.URL)
	}
	if byName["mnemo"].URL != "http://127.0.0.1:7700/mcp" {
		t.Fatalf("discovered mnemo missing: %+v", list)
	}
}
