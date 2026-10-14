// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/mcpattach"
)

// 🎯T1039: the production mint path (mountHTTPUpstreamProxy → Proxied →
// SessionServers) with a stale nested proxy in LoadMCP and the owner's
// direct map in state_dir/mcp_upstreams.json. AgentDef.MCPServers must
// receive http://127.0.0.1:18743/mcp, not :52322/upstream/bullseye and
// not this daemon's /upstream/bullseye.
func TestT1039FreshMintGrantsDirectBullseyeURL(t *testing.T) {
	state := t.TempDir()
	mcpDir := filepath.Join(state, "mcp")
	if err := os.MkdirAll(mcpDir, 0o700); err != nil {
		t.Fatal(err)
	}
	claude := filepath.Join(mcpDir, "claude.json")
	inv := map[string]any{
		"mcpServers": map[string]any{
			"bullseye":  map[string]any{"type": "http", "url": "http://127.0.0.1:52322/upstream/bullseye"},
			"atlassian": map[string]any{"type": "http", "url": "https://mcp.atlassian.com/v1/mcp/authv2"},
		},
	}
	raw, err := json.Marshal(inv)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claude, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	grant := map[string]string{
		"bullseye":  "http://127.0.0.1:18743/mcp",
		"atlassian": "https://mcp.atlassian.com/v1/mcp/authv2",
	}
	graw, err := json.MarshalIndent(grant, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "mcp_upstreams.json"), append(graw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.StateDir = state
	cfg.WorkDir = t.TempDir()
	attach := mcpattach.Args{
		Name:       "jevonsmcp",
		URL:        "http://127.0.0.1:13705/mcp",
		ClaudeJSON: claude,
		GrokTOML:   filepath.Join(mcpDir, "grok.toml"),
		CodexTOML:  filepath.Join(mcpDir, "codex.toml"),
		Isolate:    true,
	}
	mux := http.NewServeMux()
	up := mountHTTPUpstreamProxy(mux, cfg, nil, nil, "127.0.0.1", 13705, attach, nil)
	if up == nil {
		t.Fatal("mountHTTPUpstreamProxy returned nil")
	}
	attach.Proxied = up.Advertised()
	list := mcpattach.SessionServers(attach, claudia.ProviderClaude, cfg.WorkDir)
	byName := map[string]claudia.MCPServer{}
	for _, s := range list {
		byName[s.Name] = s
	}
	if byName["bullseye"].URL != "http://127.0.0.1:18743/mcp" {
		t.Fatalf("fresh mint AgentDef.MCPServers bullseye = %q; want owner direct grant", byName["bullseye"].URL)
	}
	if byName["atlassian"].URL != "http://127.0.0.1:13705/upstream/atlassian" {
		t.Fatalf("atlassian = %q; remote OAuth still takes the T520 proxy", byName["atlassian"].URL)
	}
	if byName["jevonsmcp"].URL != attach.URL {
		t.Fatalf("jevonsmcp = %+v", byName["jevonsmcp"])
	}
	onDisk, err := os.ReadFile(filepath.Join(state, "mcp_upstreams.json"))
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]string
	if err := json.Unmarshal(onDisk, &stored); err != nil {
		t.Fatal(err)
	}
	if stored["bullseye"] != "http://127.0.0.1:18743/mcp" {
		t.Fatalf("owner map poisoned: %v", stored)
	}
}
