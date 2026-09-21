// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package mcpattach is how jevonsd puts the MCP set on every Session:
// LoadMCP (discovery) + append jevonsmcp for Claude/Codex/Cursor, then
// AgentDef.MCPServers. Grok is different (🎯T525): ACP session/new gets
// only this daemon's HTTP jevonsmcp — re-sending the full inventory in a
// Claude-shaped ACP payload made session/new return Invalid params.
// Codex exclusive Launch writes CODEX_HOME from MCPServers (app-server
// thread/start has no MCP field). Claudia v0.27+ has no EnsureMCP.
package mcpattach

import (
	"fmt"
	"strings"

	"github.com/marcelocantos/claudia"
)

// Args names this daemon's HTTP MCP endpoint and, optionally, isolate
// fixture paths so LoadMCP never reads the owner's HOME maps (🎯T379).
type Args struct {
	Name string
	URL  string
	// Path overrides. Empty means the production user-scope files
	// (LoadMCP discovery + daily Scrub). Isolates point these at
	// state_dir/mcp so a journey does not inherit the owner map —
	// the files themselves are not written.
	ClaudeJSON string
	GrokTOML   string
	CodexTOML  string
	CursorJSON string
	// Isolate is a throwaway universe: skip HOME scrub; LoadMCP uses
	// the fixture paths (missing is empty). Seats still get
	// SessionServers, including Codex HTTP jevonsmcp.
	Isolate bool
	// Proxied is the T520 loopback list from mcpup.Mount.Advertised.
	// SessionServers rewrites matching HTTP URLs so seats dial
	// jevonsd, not the remote.
	Proxied []claudia.MCPServer
}

// Exclusive is Jevons's Session MCP policy: do not merge the owner's
// user-scope maps. Isolates and daily both set AgentDef.MCPExclusive.
const Exclusive = true

// StampExclusive sets MCPExclusive on def. If a Grok connect-mode leftover
// was minted before exclusive MCP, it returns true so the caller can drop
// ConnectURL/PID (that serve has no GROK_HOME). Idempotent when already set.
func StampExclusive(def *claudia.AgentDef) (dropGrokConnect bool) {
	if def == nil {
		return false
	}
	if def.MCPExclusive {
		return false
	}
	def.MCPExclusive = Exclusive
	return def.Provider == claudia.ProviderGrok && (def.ConnectURL != "" || def.ConnectPID != 0)
}

// HTTPURL is the streamable-HTTP endpoint agents dial. host must be a
// concrete address (never "localhost"); port is the served port (🎯T379).
func HTTPURL(host string, port int) string {
	return fmt.Sprintf("http://%s:%d/mcp", host, port)
}

// SessionServers is the list Jevons passes on AgentDef.MCPServers.
// Claude/Cursor/Codex get discovered system servers plus this daemon's
// jevonsmcp, with T520 loopbacks applied from Proxied. Grok gets only
// the live HTTP jevonsmcp (🎯T525).
func SessionServers(a Args, provider claudia.Provider, workDir string) []claudia.MCPServer {
	if provider == claudia.ProviderGrok {
		return grokSessionServers(a)
	}
	inv, err := claudia.LoadMCP(loadArgs(a, workDir))
	if err != nil || inv == nil {
		inv = &claudia.MCPInventory{}
	}
	if name := strings.TrimSpace(a.Name); name != "" && strings.TrimSpace(a.URL) != "" {
		inv.Servers = replaceOrAppend(inv.Servers, claudia.MCPServer{
			Name: name, Type: "http", URL: a.URL,
		})
	}
	return applyProxied(inv.ForProvider(provider), a.Proxied)
}

func grokSessionServers(a Args) []claudia.MCPServer {
	if strings.TrimSpace(a.Name) == "" || strings.TrimSpace(a.URL) == "" {
		return nil
	}
	return []claudia.MCPServer{{
		Name: a.Name,
		Type: "http",
		URL:  a.URL,
	}}
}

func loadArgs(a Args, workDir string) *claudia.LoadMCPArgs {
	explicit := a.ClaudeJSON != "" || a.GrokTOML != "" || a.CodexTOML != "" || a.CursorJSON != ""
	if !explicit {
		return &claudia.LoadMCPArgs{WorkDir: workDir}
	}
	return &claudia.LoadMCPArgs{
		ClaudeJSON: a.ClaudeJSON,
		GrokTOML:   a.GrokTOML,
		CodexTOML:  a.CodexTOML,
		CursorJSON: a.CursorJSON,
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

func applyProxied(list []claudia.MCPServer, proxied []claudia.MCPServer) []claudia.MCPServer {
	if len(proxied) == 0 {
		return list
	}
	byName := make(map[string]string, len(proxied))
	for _, p := range proxied {
		if p.Name == "" || strings.TrimSpace(p.URL) == "" {
			continue
		}
		byName[p.Name] = p.URL
	}
	if len(byName) == 0 {
		return list
	}
	out := append([]claudia.MCPServer(nil), list...)
	for i, s := range out {
		url, ok := byName[s.Name]
		if !ok {
			continue
		}
		out[i].URL = url
		if out[i].Type == "" {
			out[i].Type = "http"
		}
	}
	return out
}

// ServersEqual reports whether two MCP lists match on name, URL, and type.
func ServersEqual(a, b []claudia.MCPServer) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].URL != b[i].URL || a[i].Type != b[i].Type {
			return false
		}
	}
	return true
}
