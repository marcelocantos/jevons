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

// TestT696GrokSessionServersMatchClaudeOwnerMap is the regression for the
// 2026-09-20 finding: mcpattach.SessionServers returned only jevonsmcp for
// ProviderGrok while every other backend got the owner's full MCP map
// (mnemo, bullseye, …). Fleet Grok is OMP sidecar now; capability must not
// change silently when routing moves a seat onto grok (🎯T696).
//
// Supersedes TestT525GrokSessionServersAreOnlyJevonsHTTP: that oracle locked
// the ACP-era workaround (Claude-shaped inventory → session/new Invalid
// params). OMP host tools consume AgentDef.MCPServers; the full standard set
// is required.
func TestT696GrokSessionServersMatchClaudeOwnerMap(t *testing.T) {
	dir := t.TempDir()
	claude := filepath.Join(dir, "claude.json")
	grok := filepath.Join(dir, "grok.toml")
	codex := filepath.Join(dir, "codex.toml")
	doc := map[string]any{
		"mcpServers": map[string]any{
			"bullseye": map[string]any{"command": "/opt/homebrew/bin/mcpbridge"},
			"mnemo":    map[string]any{"type": "http", "url": "http://127.0.0.1:7700/mcp"},
			"jevonsmcp": map[string]any{
				"type": "http", "url": "http://127.0.0.1:54558/mcp",
			},
		},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claude, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(grok, []byte(""), 0o600); err != nil {
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

	claudeList := SessionServers(a, claudia.ProviderClaude, "")
	grokList := SessionServers(a, claudia.ProviderGrok, "")
	xaioList := SessionServers(a, "xai-oauth", "")

	assertOwnerMap := func(t *testing.T, label string, list []claudia.MCPServer) {
		t.Helper()
		byName := map[string]claudia.MCPServer{}
		for _, s := range list {
			byName[s.Name] = s
		}
		if byName["mnemo"].URL != "http://127.0.0.1:7700/mcp" {
			t.Fatalf("%s dropped mnemo: %+v", label, list)
		}
		if byName["bullseye"].Command == "" {
			t.Fatalf("%s dropped bullseye: %+v", label, list)
		}
		if byName["jevonsmcp"].URL != a.URL || byName["jevonsmcp"].Type != "http" {
			t.Fatalf("%s jevonsmcp = %+v; want live HTTP %s", label, byName["jevonsmcp"], a.URL)
		}
		if len(list) < 3 {
			t.Fatalf("%s returned only %+v; want full owner map, not jevonsmcp-only (🎯T696 / 2026-09-20)", label, list)
		}
	}

	assertOwnerMap(t, "claude", claudeList)
	assertOwnerMap(t, "grok", grokList)
	assertOwnerMap(t, "xai-oauth", xaioList)

	if !ServersEqual(claudeList, grokList) {
		t.Fatalf("grok SessionServers diverged from claude:\n  claude=%+v\n  grok=%+v", claudeList, grokList)
	}
	if !ServersEqual(claudeList, xaioList) {
		t.Fatalf("xai-oauth SessionServers diverged from claude:\n  claude=%+v\n  xai=%+v", claudeList, xaioList)
	}
}
