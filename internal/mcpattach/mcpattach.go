// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package mcpattach is how jevonsd puts a hermetic MCP set on every
// Session: Config.MCPServers = [jevonsmcp HTTP]. That list is closed —
// it does not LoadMCP user configs (🎯T525). Codex still needs
// EnsureMCP because app-server thread/start has no MCP field.
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

// SessionServers is the hermetic set for AgentDef.MCPServers: this
// daemon's jevonsmcp HTTP endpoint only. User config.toml / claude.json
// inventories stay off the Session (🎯T525). workDir is unused; the
// signature keeps the fleet call site stable.
func SessionServers(a Args, provider claudia.Provider, workDir string) []claudia.MCPServer {
	_ = workDir
	if strings.TrimSpace(a.Name) == "" || strings.TrimSpace(a.URL) == "" {
		return nil
	}
	if a.Isolate && provider == claudia.ProviderCodex {
		return nil
	}
	return []claudia.MCPServer{{
		Name: a.Name,
		Type: "http",
		URL:  a.URL,
	}}
}
