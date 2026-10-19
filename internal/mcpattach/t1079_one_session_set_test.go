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

// TestT1079EverySeatGetsTheStandardSet retires the 🎯T525 Grok exception.
// That oracle pinned a grok seat to jevonsmcp alone because the Grok ACP
// CLI's session/new rejected a Claude-shaped inventory. Claudia v0.58 runs
// grok on the Oh My Pi sidecar (the CLI transport is refused), and the
// exception had come to depend on spelling: the overseer row stored grok and
// got one server while the same seat stored as xai-oauth got the full set.
// The list is now one list: discovered servers, stdio and HTTP alike (the
// sidecar registers the HTTP ones and skips the rest), plus the live
// jevonsmcp.
func TestT1079EverySeatGetsTheStandardSet(t *testing.T) {
	dir := t.TempDir()
	claude := filepath.Join(dir, "claude.json")
	grok := filepath.Join(dir, "grok.toml")
	codex := filepath.Join(dir, "codex.toml")
	doc := map[string]any{
		"mcpServers": map[string]any{
			"bullseye":  map[string]any{"command": "/opt/homebrew/bin/mcpbridge"},
			"mnemo":     map[string]any{"type": "http", "url": "http://127.0.0.1:19419/mcp"},
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
	list := SessionServers(a, "")
	byName := map[string]claudia.MCPServer{}
	for _, s := range list {
		byName[s.Name] = s
	}
	if len(list) != 3 || byName["bullseye"].Command == "" || byName["mnemo"].URL == "" {
		t.Fatalf("SessionServers = %+v; want the discovered bullseye and mnemo plus jevonsmcp", list)
	}
	if byName["jevonsmcp"].URL != a.URL || byName["jevonsmcp"].Type != "http" {
		t.Fatalf("jevonsmcp = %+v; want the live URL %s, not the stale map entry", byName["jevonsmcp"], a.URL)
	}
}
