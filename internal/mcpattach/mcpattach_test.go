// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpattach

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

func fixtureArgs(t *testing.T, name, url string) Args {
	t.Helper()
	dir := t.TempDir()
	return Args{
		Name:       name,
		URL:        url,
		ClaudeJSON: filepath.Join(dir, "claude.json"),
		GrokTOML:   filepath.Join(dir, "grok.toml"),
		CodexTOML:  filepath.Join(dir, "codex.toml"),
	}
}

func writeNamedHTTP(t *testing.T, a Args, name, url string) {
	t.Helper()
	doc := map[string]any{
		"mcpServers": map[string]any{
			name: map[string]any{"type": "http", "url": url},
		},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.ClaudeJSON, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if a.CursorJSON != "" {
		if err := os.WriteFile(a.CursorJSON, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	toml := "[mcp_servers." + name + "]\nurl = \"" + url + "\"\n"
	if err := os.WriteFile(a.GrokTOML, []byte(toml), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.CodexTOML, []byte(toml), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSessionServersAppendsJevonsmcpAndKeepsDiscovered(t *testing.T) {
	a := fixtureArgs(t, "jevonsmcp", "http://127.0.0.1:13705/mcp")
	doc := map[string]any{
		"mcpServers": map[string]any{
			"mnemo": map[string]any{"type": "http", "url": "http://127.0.0.1:7700/mcp"},
		},
	}
	raw, _ := json.Marshal(doc)
	if err := os.WriteFile(a.ClaudeJSON, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	list := SessionServers(a, claudia.ProviderClaude, "")
	var names []string
	var jevonsURL string
	for _, s := range list {
		names = append(names, s.Name)
		if s.Name == "jevonsmcp" {
			jevonsURL = s.URL
		}
	}
	joined := strings.Join(names, ",")
	if !strings.Contains(joined, "mnemo") {
		t.Fatalf("discovered mnemo dropped: %v", names)
	}
	if jevonsURL != a.URL {
		t.Fatalf("jevonsmcp url = %q", jevonsURL)
	}
}

func TestSessionServersReplacesStaleJevonsmcp(t *testing.T) {
	a := fixtureArgs(t, "jevonsmcp", "http://127.0.0.1:13705/mcp")
	doc := map[string]any{
		"mcpServers": map[string]any{
			"jevonsmcp": map[string]any{"type": "http", "url": "http://127.0.0.1:54558/mcp"},
		},
	}
	raw, _ := json.Marshal(doc)
	if err := os.WriteFile(a.ClaudeJSON, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	list := SessionServers(a, claudia.ProviderClaude, "")
	if len(list) != 1 || list[0].URL != a.URL {
		t.Fatalf("want one live jevonsmcp, got %+v", list)
	}
}

func TestIsolateCodexSessionKeepsHTTP(t *testing.T) {
	a := fixtureArgs(t, "jevonsmcp-journey", "http://127.0.0.1:13715/mcp")
	a.Isolate = true
	got := SessionServers(a, claudia.ProviderCodex, "")
	if len(got) != 1 || got[0].Name != "jevonsmcp-journey" || got[0].URL != a.URL {
		t.Fatalf("isolate Codex SessionServers = %+v; want journey HTTP jevonsmcp", got)
	}
	claude := SessionServers(a, claudia.ProviderClaude, "")
	if len(claude) != 1 || claude[0].Name != "jevonsmcp-journey" {
		t.Fatalf("isolate Claude should still carry the journey server: %+v", claude)
	}
}

func TestSessionServersRewritesProxiedHTTP(t *testing.T) {
	a := fixtureArgs(t, "jevonsmcp", "http://127.0.0.1:13705/mcp")
	doc := map[string]any{
		"mcpServers": map[string]any{
			"atlassian": map[string]any{"type": "http", "url": "https://mcp.atlassian.com/v1/mcp"},
		},
	}
	raw, _ := json.Marshal(doc)
	if err := os.WriteFile(a.ClaudeJSON, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	a.Proxied = []claudia.MCPServer{{
		Name: "atlassian", Type: "http", URL: "http://127.0.0.1:13705/upstream/atlassian",
	}}
	list := SessionServers(a, claudia.ProviderClaude, "")
	byName := map[string]claudia.MCPServer{}
	for _, s := range list {
		byName[s.Name] = s
	}
	if byName["atlassian"].URL != "http://127.0.0.1:13705/upstream/atlassian" {
		t.Fatalf("proxied atlassian = %+v", byName["atlassian"])
	}
	if byName["jevonsmcp"].URL != a.URL {
		t.Fatalf("jevonsmcp = %+v", byName["jevonsmcp"])
	}
}

func TestScrubRemovesJSONAndTOML(t *testing.T) {
	a := fixtureArgs(t, "jevonsmcp", "http://127.0.0.1:13705/mcp")
	a.CursorJSON = filepath.Join(filepath.Dir(a.ClaudeJSON), "cursor.json")
	writeNamedHTTP(t, a, a.Name, a.URL)
	if err := Scrub(a); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{a.ClaudeJSON, a.GrokTOML, a.CodexTOML, a.CursorJSON} {
		b, err := os.ReadFile(p)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "jevonsmcp") {
			t.Fatalf("%s still has jevonsmcp: %s", p, b)
		}
	}
}

func TestHTTPURL(t *testing.T) {
	if got := HTTPURL("127.0.0.1", 13705); got != "http://127.0.0.1:13705/mcp" {
		t.Fatalf("got %q", got)
	}
}
