// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package mcpattach is how jevonsd puts the MCP set on every Session:
// LoadMCP (discovery) + append jevonsmcp for every backend, then
// AgentDef.MCPServers. Provider choice must not silently drop history
// tools (🎯T696): Grok/xai-oauth seats get the same owner MCP map
// (mnemo, bullseye, …) as Claude. The old Grok ACP-only jevonsmcp list
// (🎯T525) was for Claude-shaped ACP session/new Invalid params; fleet
// Grok is OMP sidecar now and consumes MCPServers as host tools.
// Codex exclusive Launch writes CODEX_HOME from MCPServers (app-server
// thread/start has no MCP field). Claudia v0.27+ has no EnsureMCP.
package mcpattach

import (
	"fmt"
	"strings"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/mcpup"
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
	// Proxied is the grant list from mcpup.Mount.Advertised.
	// SessionServers rewrites matching HTTP URLs. Remote OAuth keeps
	// the T520 jevonsd /upstream/ loopback; a local loopback MCP is
	// granted its direct URL (🎯T1039) so a nested leftover cannot
	// be persisted onto AgentDef.MCPServers.
	Proxied []claudia.MCPServer
}

// Exclusive is Jevons's Session MCP policy: do not merge the owner's
// user-scope maps. Isolates and daily both set AgentDef.MCPExclusive.
const Exclusive = true

// HTTPURL is the streamable-HTTP endpoint agents dial. host must be a
// concrete address (never "localhost"); port is the served port (🎯T379).
func HTTPURL(host string, port int) string {
	return fmt.Sprintf("http://%s:%d/mcp", host, port)
}

// SessionServers is the list Jevons passes on AgentDef.MCPServers.
// Every backend — including ProviderGrok and xai-oauth — uses Claude's
// LoadMCP inventory as the standard set (🎯T871 / 🎯T696), plus this
// daemon's jevonsmcp, with T520/T1039 grants applied from Proxied.
// computer-use is dropped. Routing a seat to grok must not strip
// mnemo-backed briefing context (🎯T696).
func SessionServers(a Args, provider claudia.Provider, workDir string) []claudia.MCPServer {
	_ = provider // policy is provider-uniform (🎯T696); kept for call-site clarity
	inv, err := claudia.LoadMCP(loadArgs(a, workDir))
	if err != nil || inv == nil {
		inv = &claudia.MCPInventory{}
	}
	if name := strings.TrimSpace(a.Name); name != "" && strings.TrimSpace(a.URL) != "" {
		inv.Servers = replaceOrAppend(inv.Servers, claudia.MCPServer{
			Name: name, Type: "http", URL: a.URL,
		})
	}
	list := applyProxied(dropComputerUseMCP(inv.ForProvider(claudia.ProviderClaude)), a.Proxied)
	return ensurePlaywrightIsolated(list)
}

// dropComputerUseMCP removes computer-use, the one Claude/Codex MCP
// that is not part of the shared harness set (🎯T871).
func dropComputerUseMCP(list []claudia.MCPServer) []claudia.MCPServer {
	out := make([]claudia.MCPServer, 0, len(list))
	for _, s := range list {
		if isComputerUseMCP(s.Name) {
			continue
		}
		out = append(out, s)
	}
	return out
}

func isComputerUseMCP(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "computer-use", "computer_use":
		return true
	default:
		return false
	}
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
		// 🎯T1039: a direct loopback grant is not rewritten to a nested
		// /upstream/ leftover. Remote OAuth (non-loopback) still takes
		// the T520 proxy URL.
		if mcpup.GrantDirect(s.URL) && mcpup.IsNestedProxyURL(url) {
			continue
		}
		out[i].URL = url
		if out[i].Type == "" {
			out[i].Type = "http"
		}
	}
	return out
}

// ensurePlaywrightIsolated makes Playwright MCP isolation the fleet default
// (🎯T430). A shared --user-data-dir profile is a single-holder resource:
// the second concurrent worker is refused with "Browser is already in use
// … use --isolated". Relying on every owner map to remember --isolated is
// the convention 🎯T405 clause 2 rejects. This rewrite is the product
// property: missing isolation cannot mint a seat that locks siblings out.
//
// Policy: drop every --user-data-dir <path> pair (the shared-profile form
// that collides), then ensure --isolated is present. Non-playwright
// servers are untouched. Idempotent when already isolated.
func ensurePlaywrightIsolated(list []claudia.MCPServer) []claudia.MCPServer {
	if len(list) == 0 {
		return list
	}
	out := append([]claudia.MCPServer(nil), list...)
	for i, s := range out {
		if !isPlaywrightMCP(s) {
			continue
		}
		out[i].Args = playwrightIsolatedArgs(s.Args)
	}
	return out
}

func isPlaywrightMCP(s claudia.MCPServer) bool {
	name := strings.ToLower(strings.TrimSpace(s.Name))
	if name == "playwright" || strings.HasPrefix(name, "playwright-") {
		return true
	}
	// Nameless / misnamed entries that still launch the Playwright MCP
	// package must not keep a shared profile either.
	blob := strings.ToLower(strings.TrimSpace(s.Command) + " " + strings.Join(s.Args, " "))
	return strings.Contains(blob, "@playwright/mcp") || strings.Contains(blob, "playwright-mcp")
}

func playwrightIsolatedArgs(args []string) []string {
	out := make([]string, 0, len(args)+1)
	hasIsolated := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--isolated" || a == "-isolated":
			if !hasIsolated {
				out = append(out, "--isolated")
				hasIsolated = true
			}
		case a == "--user-data-dir" || strings.HasPrefix(a, "--user-data-dir="):
			// Drop the flag and its path argument when separate.
			if a == "--user-data-dir" && i+1 < len(args) {
				i++
			}
			// integrated --user-data-dir=path form: just skip
		default:
			out = append(out, a)
		}
	}
	if !hasIsolated {
		out = append(out, "--isolated")
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
		if a[i].Command != b[i].Command {
			return false
		}
		if !stringSlicesEqual(a[i].Args, b[i].Args) {
			return false
		}
	}
	return true
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
