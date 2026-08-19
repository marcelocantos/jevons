// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package mcpattach is how jevonsd puts jevonsmcp on every Session
// backend: LoadMCP (discovery) + append + EnsureMCP (native configs).
// Callers never write ~/.claude.json, ~/.grok/config.toml, or
// ~/.codex/config.toml themselves (claudia 🎯T40).
package mcpattach

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/marcelocantos/claudia"
)

// Args names this daemon's HTTP MCP endpoint and, optionally, isolate
// fixture paths so Ensure/Load never touch the daily user files (🎯T379).
type Args struct {
	Name string
	URL  string
	// Path overrides. Empty means the production user-scope files.
	// Isolates and tests must set all three.
	ClaudeJSON string
	GrokTOML   string
	CodexTOML  string
	// Isolate is true for a throwaway universe: SessionServers omits
	// HTTP entries on Codex so claudia.Launch does not EnsureMCP into
	// the owner's ~/.codex/config.toml.
	Isolate bool
}

// HTTPURL is the streamable-HTTP endpoint agents dial. host must be a
// concrete address (never "localhost"); port is the served port (🎯T379).
func HTTPURL(host string, port int) string {
	return fmt.Sprintf("http://%s:%d/mcp", host, port)
}

// Ensure writes Name+URL into each Session provider's own config via
// claudia.EnsureMCP. Isolates/tests pass fixture paths.
func Ensure(a Args) error {
	if strings.TrimSpace(a.Name) == "" || strings.TrimSpace(a.URL) == "" {
		return fmt.Errorf("mcpattach: name and url required")
	}
	for _, p := range []string{a.ClaudeJSON, a.GrokTOML, a.CodexTOML} {
		if p == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return fmt.Errorf("mcpattach: mkdir %s: %w", filepath.Dir(p), err)
		}
	}
	return claudia.EnsureMCP(&claudia.EnsureMCPArgs{
		Name:       a.Name,
		URL:        a.URL,
		ClaudeJSON: a.ClaudeJSON,
		GrokTOML:   a.GrokTOML,
		CodexTOML:  a.CodexTOML,
	})
}

// SessionServers is the list to put on AgentDef.MCPServers / Config.MCPServers:
// discovered system servers, with this daemon's jevonsmcp appended (or
// replacing a stale same-name entry). Caller-appended jevonsmcp has empty
// Providers so ForProvider includes it for every backend.
func SessionServers(a Args, provider claudia.Provider, workDir string) []claudia.MCPServer {
	inv, err := claudia.LoadMCP(loadArgs(a, workDir))
	if err != nil || inv == nil {
		inv = &claudia.MCPInventory{}
	}
	if name := strings.TrimSpace(a.Name); name != "" && strings.TrimSpace(a.URL) != "" {
		inv.Servers = replaceOrAppend(inv.Servers, claudia.MCPServer{
			Name: name, Type: "http", URL: a.URL,
		})
	}
	list := inv.ForProvider(provider)
	if a.Isolate && provider == claudia.ProviderCodex {
		return stripHTTP(list)
	}
	return list
}

func loadArgs(a Args, workDir string) *claudia.LoadMCPArgs {
	explicit := a.ClaudeJSON != "" || a.GrokTOML != "" || a.CodexTOML != ""
	if !explicit {
		return &claudia.LoadMCPArgs{WorkDir: workDir}
	}
	return &claudia.LoadMCPArgs{
		ClaudeJSON: a.ClaudeJSON,
		GrokTOML:   a.GrokTOML,
		CodexTOML:  a.CodexTOML,
		WorkDir:    workDir,
	}
}

func replaceOrAppend(list []claudia.MCPServer, mine claudia.MCPServer) []claudia.MCPServer {
	out := append([]claudia.MCPServer(nil), list...)
	for i, s := range out {
		if s.Name == mine.Name {
			out[i] = mine
			return out
		}
	}
	return append(out, mine)
}

func stripHTTP(list []claudia.MCPServer) []claudia.MCPServer {
	var out []claudia.MCPServer
	for _, s := range list {
		if strings.TrimSpace(s.URL) != "" {
			continue
		}
		out = append(out, s)
	}
	return out
}
