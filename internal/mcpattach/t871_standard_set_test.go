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

// 🎯T871 acceptance hermetics beyond the xai-oauth / computer-use cases
// already in mcpattach_test.go:
//   - every non-Grok provider id (anthropic, openai-codex, cursor, …)
//     carries Claude's LoadMCP inventory plus jevonsmcp
//   - a server that lives only in grok.toml or cursor mcp.json is absent
//   - missing isolate fixture files yield jevonsmcp only (T379)

func TestT871EveryNonGrokProviderGetsClaudeStandardSet(t *testing.T) {
	a := fixtureArgs(t, "jevonsmcp", "http://127.0.0.1:13705/mcp")
	doc := map[string]any{
		"mcpServers": map[string]any{
			"mnemo": map[string]any{"type": "http", "url": "http://127.0.0.1:7700/mcp"},
		},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.ClaudeJSON, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	// Provider ids used on seats today. ProviderGrok ("grok") is the
	// ACP special case (T525) and is covered separately.
	providers := []claudia.Provider{
		claudia.ProviderClaude,
		claudia.ProviderCodex,
		claudia.ProviderCursor,
		"anthropic",
		"openai-codex",
		"xai-oauth",
		"cursor",
	}
	for _, p := range providers {
		t.Run(string(p), func(t *testing.T) {
			list := SessionServers(a, p, "")
			byName := map[string]claudia.MCPServer{}
			for _, s := range list {
				byName[s.Name] = s
			}
			if byName["mnemo"].URL != "http://127.0.0.1:7700/mcp" {
				t.Fatalf("%s dropped Claude mnemo: %+v", p, list)
			}
			if byName["jevonsmcp"].URL != a.URL {
				t.Fatalf("%s jevonsmcp = %+v; want live URL %s", p, byName["jevonsmcp"], a.URL)
			}
		})
	}
}

func TestT871ServerOnlyInGrokOrCursorMapIsAbsent(t *testing.T) {
	dir := t.TempDir()
	claude := filepath.Join(dir, "claude.json")
	grok := filepath.Join(dir, "grok.toml")
	codex := filepath.Join(dir, "codex.toml")
	cursor := filepath.Join(dir, "cursor.json")

	// Claude map has only mnemo. Grok and Cursor each carry a private
	// server that must NOT appear on the standard worker set.
	if err := os.WriteFile(claude, []byte(`{"mcpServers":{"mnemo":{"type":"http","url":"http://127.0.0.1:7700/mcp"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(grok, []byte(`
[mcp_servers.grok-only]
url = "http://127.0.0.1:9999/mcp"
enabled = true
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codex, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cursor, []byte(`{"mcpServers":{"cursor-only":{"type":"http","url":"http://127.0.0.1:8888/mcp"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	a := Args{
		Name:       "jevonsmcp",
		URL:        "http://127.0.0.1:13705/mcp",
		ClaudeJSON: claude,
		GrokTOML:   grok,
		CodexTOML:  codex,
		CursorJSON: cursor,
	}
	for _, p := range []claudia.Provider{"xai-oauth", claudia.ProviderCodex, claudia.ProviderCursor, "anthropic"} {
		list := SessionServers(a, p, "")
		byName := map[string]claudia.MCPServer{}
		for _, s := range list {
			byName[s.Name] = s
		}
		if _, ok := byName["grok-only"]; ok {
			t.Fatalf("%s leaked grok-only server: %+v", p, list)
		}
		if _, ok := byName["cursor-only"]; ok {
			t.Fatalf("%s leaked cursor-only server: %+v", p, list)
		}
		if byName["mnemo"].URL == "" {
			t.Fatalf("%s dropped Claude mnemo: %+v", p, list)
		}
		if byName["jevonsmcp"].URL != a.URL {
			t.Fatalf("%s jevonsmcp = %+v", p, byName["jevonsmcp"])
		}
	}
}

func TestT871MissingIsolateFixturesYieldJevonsmcpOnly(t *testing.T) {
	// Explicit paths that do not exist: LoadMCP must not fall through
	// to the owner HOME map (T379). The seat still gets this daemon's
	// jevonsmcp.
	dir := t.TempDir()
	a := Args{
		Name:       "jevonsmcp-journey",
		URL:        "http://127.0.0.1:13715/mcp",
		ClaudeJSON: filepath.Join(dir, "missing-claude.json"),
		GrokTOML:   filepath.Join(dir, "missing-grok.toml"),
		CodexTOML:  filepath.Join(dir, "missing-codex.toml"),
		CursorJSON: filepath.Join(dir, "missing-cursor.json"),
		Isolate:    true,
	}
	for _, p := range []claudia.Provider{"xai-oauth", claudia.ProviderCodex, claudia.ProviderClaude, "anthropic"} {
		list := SessionServers(a, p, "")
		if len(list) != 1 || list[0].Name != a.Name || list[0].URL != a.URL {
			t.Fatalf("%s with missing fixtures = %+v; want only journey jevonsmcp", p, list)
		}
	}
	// Grok ACP remains jevonsmcp-only either way (T525).
	grok := SessionServers(a, claudia.ProviderGrok, "")
	if len(grok) != 1 || grok[0].Name != a.Name || grok[0].URL != a.URL {
		t.Fatalf("Grok missing fixtures = %+v; want only journey jevonsmcp", grok)
	}
}
