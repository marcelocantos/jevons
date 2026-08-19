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

// TestT525GrokSessionServersAreOnlyJevonsHTTP is the iterate oracle for
// 🎯T525. Daily boot died on `acp session/new: Invalid params` after
// SessionServers dumped Claude/Grok inventory onto the overseer ACP
// mcpServers field. Grok already attaches ~/.grok/config.toml (T58).
// The ACP list must be this daemon's HTTP jevonsmcp only — not
// bullseye-as-command or other Claude-shaped stdio cousins.
func TestT525GrokSessionServersAreOnlyJevonsHTTP(t *testing.T) {
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

[mcp_servers.bullseye]
command = "/opt/homebrew/bin/mcpbridge"
args = ["connect", "/tmp/bullseye.json"]
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
	if len(list) != 1 || list[0].Name != "jevonsmcp" || list[0].URL != a.URL || list[0].Type != "http" {
		t.Fatalf("Grok SessionServers = %+v; want only jevonsmcp HTTP at the live URL (🎯T525)", list)
	}

	// Control: Claude Session still receives the full ForProvider list
	// (discovered bullseye + live jevonsmcp), not the Grok-only set.
	claudeList := SessionServers(a, claudia.ProviderClaude, "")
	byName := map[string]claudia.MCPServer{}
	for _, s := range claudeList {
		byName[s.Name] = s
	}
	if byName["bullseye"].Command == "" {
		t.Fatalf("Claude Session dropped bullseye: %+v", claudeList)
	}
	if byName["jevonsmcp"].URL != a.URL {
		t.Fatalf("Claude jevonsmcp = %+v; want live URL %s", byName["jevonsmcp"], a.URL)
	}
}
