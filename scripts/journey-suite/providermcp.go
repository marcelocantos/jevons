// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os/exec"
	"strings"

	"github.com/marcelocantos/claudia"
)

// Provider-aware user-scoped MCP inspection for the journey isolate (🎯T282).
//
// jevonsd registers its MCP endpoint with whichever CLI runs the overseer
// (`grok mcp add` → ~/.grok/config.toml, `claude mcp add -s user`). The suite
// must therefore list/remove through the SAME CLI as the provider under test:
// a Claude run that tore down with `grok mcp remove` would leave the journey
// server registered in the owner's Claude config and would report the daily
// registration missing.

// mcpCLI returns the CLI binary name that owns user-scoped MCP registration
// for a provider, or "" when the provider has no known install path
// (codex/bedrock — jevonsd itself reports that residual at boot).
func mcpCLI(provider claudia.Provider) string {
	switch provider {
	case claudia.ProviderClaude:
		return "claude"
	case claudia.ProviderCodex, claudia.ProviderBedrock:
		return ""
	default:
		return "grok"
	}
}

// mcpRemoveArgs differs per CLI: Claude needs an explicit user scope, while
// Grok's config.toml registration is user-scoped already. (`mcp list` is
// spelled the same for both.)
func mcpRemoveArgs(provider claudia.Provider, name string) []string {
	if mcpCLI(provider) == "claude" {
		return []string{"mcp", "remove", "-s", "user", name}
	}
	return []string{"mcp", "remove", name}
}

// mcpListedFor reports whether name appears in the provider CLI's MCP list.
// A missing CLI is reported as "not listed" rather than an error: the caller
// uses this for baseline comparison, and an absent CLI cannot hold state.
func mcpListedFor(provider claudia.Provider, name string) bool {
	cli := mcpCLI(provider)
	if cli == "" {
		return false
	}
	bin, err := exec.LookPath(cli)
	if err != nil {
		return false
	}
	out, err := exec.Command(bin, "mcp", "list").CombinedOutput()
	if err != nil {
		return false
	}
	// Lines look like: "  jevonsmcp: http://127.0.0.1:13705/mcp"
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, name+":") || strings.HasPrefix(line, name+" ") {
			return true
		}
	}
	return false
}

// mcpRemoveFor deletes the journey MCP registration from the provider CLI.
// Best-effort: teardown must never fail the suite.
func mcpRemoveFor(provider claudia.Provider, name string) {
	cli := mcpCLI(provider)
	if cli == "" {
		return
	}
	bin, err := exec.LookPath(cli)
	if err != nil {
		return
	}
	_ = exec.Command(bin, mcpRemoveArgs(provider, name)...).Run()
}
