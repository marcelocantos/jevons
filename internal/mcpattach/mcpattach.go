// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package mcpattach is how jevonsd puts the MCP set on every Session:
// LoadMCP (discovery) + this daemon's jevonsmcp, then AgentDef.MCPServers.
// The set is the same for every seat, whichever provider it runs on
// (🎯T1079). Grok used to differ (🎯T525): the Grok ACP CLI's session/new
// returned Invalid params on a Claude-shaped inventory, so a grok seat got
// jevonsmcp alone. Claudia v0.58 runs every subscription plan, grok
// included, on the Oh My Pi sidecar and refuses the CLI transports, so that
// payload no longer exists — and the exception had come to depend on
// spelling: a row storing the plan name grok got one server, the same seat
// storing xai-oauth got the full set. The sidecar offers a seat the tools
// of the HTTP servers on the list; a server that does not answer within
// claudia's host-tools budget is left out of that launch and named in
// Agent.HostMCPUnavailable (🎯T934), never a launch failure.
// Claudia v0.27+ has no EnsureMCP.
package mcpattach

import (
	"fmt"
	"os"
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

// SessionServers is the list Jevons passes on AgentDef.MCPServers: Claude's
// LoadMCP inventory as the standard set (🎯T871), plus this daemon's
// jevonsmcp, with T520/T1039 grants applied from Proxied. Every seat gets
// the same list, the overseer included (🎯T1079); it does not depend on the
// provider, so it takes none.
func SessionServers(a Args, workDir string) []claudia.MCPServer {
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
	return leadWithFleetServers(ensurePlaywrightIsolated(list, workDir), a.Name)
}

// leadWithFleetServers puts this daemon's own server first and Playwright
// second, keeping the rest in inventory order. A provider with a host-tool
// budget (claudia caps openai-codex at 250) leaves servers out from the end
// of the list; a seat without jevonsmcp cannot report or be directed, so it
// must never be the one dropped. It was last, which made it first to go.
func leadWithFleetServers(list []claudia.MCPServer, own string) []claudia.MCPServer {
	out := make([]claudia.MCPServer, 0, len(list))
	rank := func(s claudia.MCPServer) int {
		switch {
		case own != "" && s.Name == own:
			return 0
		case isPlaywrightMCP(s):
			return 1
		default:
			return 2
		}
	}
	for r := 0; r <= 2; r++ {
		for _, s := range list {
			if rank(s) == r {
				out = append(out, s)
			}
		}
	}
	return out
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
func ensurePlaywrightIsolated(list []claudia.MCPServer, workDir string) []claudia.MCPServer {
	if len(list) == 0 {
		return list
	}
	out := append([]claudia.MCPServer(nil), list...)
	for i, s := range out {
		if !isPlaywrightMCP(s) {
			continue
		}
		out[i].Args = playwrightIsolatedArgs(s.Args)
		// 🎯T1239: a stdio Playwright MCP inherits its launcher's cwd as
		// its workspace root. That launcher is the seat's own process,
		// whose cwd is the seat's workDir — a git checkout for every
		// Build seat. Move it to a per-seat scratch dir outside any
		// checkout so a screenshot (named or auto-named) cannot land as
		// untracked litter in a shared clone.
		if out[i].Command != "" {
			scratch := playwrightScratchDir(workDir)
			if err := os.MkdirAll(scratch, 0o755); err == nil {
				command, args := unwrapScratchCwd(out[i].Command, out[i].Args)
				out[i].Command, out[i].Args = wrapCommandWithCwd(command, args, scratch)
			}
		}
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
	out = playwrightFleetBrowserArgs(out)
	return out
}

// playwrightFleetBrowserArgs stamps fleet Playwright sessions so owner-origin
// cockpit sends are refused (🎯T1129).
func playwrightFleetBrowserArgs(args []string) []string {
	const uaFlag = "--user-agent"
	for i, a := range args {
		if a == uaFlag && i+1 < len(args) {
			return args
		}
		if strings.HasPrefix(a, uaFlag+"=") {
			return args
		}
	}
	out := append([]string(nil), args...)
	out = append(out, uaFlag, "JevonsFleetBrowser/1 (Playwright MCP; not the owner)")
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
