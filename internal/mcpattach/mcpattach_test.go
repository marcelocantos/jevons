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

func TestEnsureWritesAllThreeAndIsIdempotent(t *testing.T) {
	a := fixtureArgs(t, "jevonsmcp", "http://127.0.0.1:13705/mcp")
	if err := Ensure(a); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(a); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(a.ClaudeJSON)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "127.0.0.1:13705/mcp") {
		t.Fatalf("claude json missing url: %s", raw)
	}
	for _, p := range []string{a.GrokTOML, a.CodexTOML} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), "127.0.0.1:13705/mcp") {
			t.Fatalf("%s missing url: %s", p, b)
		}
	}
}

func TestEnsureCorrectsStaleURL(t *testing.T) {
	a := fixtureArgs(t, "jevonsmcp", "http://127.0.0.1:54558/mcp")
	if err := Ensure(a); err != nil {
		t.Fatal(err)
	}
	a.URL = "http://127.0.0.1:13705/mcp"
	if err := Ensure(a); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(a.ClaudeJSON)
	if strings.Contains(string(raw), "54558") {
		t.Fatalf("stale port survived: %s", raw)
	}
	if !strings.Contains(string(raw), "13705") {
		t.Fatalf("live port missing: %s", raw)
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

func TestIsolateCodexSessionOmitsHTTP(t *testing.T) {
	a := fixtureArgs(t, "jevonsmcp-journey", "http://127.0.0.1:13715/mcp")
	a.Isolate = true
	if err := Ensure(a); err != nil {
		t.Fatal(err)
	}
	if got := SessionServers(a, claudia.ProviderCodex, ""); len(got) != 0 {
		t.Fatalf("isolate Codex SessionServers = %+v; want empty so Launch does not write ~/.codex", got)
	}
	claude := SessionServers(a, claudia.ProviderClaude, "")
	if len(claude) != 1 || claude[0].Name != "jevonsmcp-journey" {
		t.Fatalf("isolate Claude should still carry the journey server: %+v", claude)
	}
}

func TestHTTPURL(t *testing.T) {
	if got := HTTPURL("127.0.0.1", 13705); got != "http://127.0.0.1:13705/mcp" {
		t.Fatalf("got %q", got)
	}
}
