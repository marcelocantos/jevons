// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"log/slog"
	"path/filepath"

	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/mcpattach"
	"github.com/marcelocantos/jevons/internal/mcpscope"
)

// registerMCPEndpoints puts jevonsmcp on every Session backend after bind
// (🎯T379: URL from the live listener). Claudia EnsureMCP writes Claude,
// Grok, and Codex native configs; LoadMCP+append is the session list.
// Isolates write only under state_dir/mcp so they cannot leak a throwaway
// port into the owner's user-scope files.
func registerMCPEndpoints(cfg config.Config, host string, port int) mcpattach.Args {
	return registerMCPEndpointsAt(cfg, host, port, fleetMCPAttach(cfg, host, port))
}

func fleetMCPAttach(cfg config.Config, host string, port int) mcpattach.Args {
	a := mcpattach.Args{
		Name: fleetMCPName(cfg),
		URL:  mcpattach.HTTPURL(host, port),
	}
	if config.IsDailyStateDir(cfg.StateDir) {
		return a
	}
	dir := filepath.Join(cfg.StateDir, "mcp")
	a.ClaudeJSON = filepath.Join(dir, "claude.json")
	a.GrokTOML = filepath.Join(dir, "grok.toml")
	a.CodexTOML = filepath.Join(dir, "codex.toml")
	a.Isolate = true
	return a
}

func registerMCPEndpointsAt(cfg config.Config, host string, port int, a mcpattach.Args) mcpattach.Args {
	if a.Name == "" {
		a = fleetMCPAttach(cfg, host, port)
	}
	if err := mcpattach.Ensure(a); err != nil {
		slog.Warn("could not ensure jevonsmcp on provider configs — agents may start without jevons tools",
			"name", a.Name, "url", a.URL, "err", err)
		return a
	}
	slog.Info("jevonsmcp ensured on Claude, Grok, and Codex configs",
		"name", a.Name, "url", a.URL, "isolate", a.Isolate)
	return a
}

func fleetMCPName(cfg config.Config) string {
	if cfg.MCPServerName == "" {
		return mcpscope.ServerName
	}
	return cfg.MCPServerName
}

// overseerMCPServerSpec is the name+URL advertised after bind (🎯T58/T379).
func overseerMCPServerSpec(cfg config.Config, host string, port int) (name, url string) {
	return fleetMCPName(cfg), mcpattach.HTTPURL(host, port)
}
