// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/mcpscope"
)

// fleetMCPAction is what the user-scope ensure did, named so the oracle can
// assert on the decision rather than on a log line.
type fleetMCPAction string

const (
	// fleetMCPSkipped: not the daily universe, so this daemon does not own
	// the user-scope entry and wrote nothing.
	fleetMCPSkipped fleetMCPAction = "skipped"
	// fleetMCPRepaired: the registration was missing or pointed elsewhere.
	fleetMCPRepaired fleetMCPAction = "repaired"
	// fleetMCPCurrent: already user-scoped and correct; the hot file was not
	// opened for writing at all.
	fleetMCPCurrent fleetMCPAction = "current"
)

// ensureFleetMCPUserScope registers this daemon's MCP endpoint in the user
// scope of ~/.claude.json, so that a Claude agent has fleet control in
// whatever directory it is started in (🎯T464).
//
// ensureOverseerMCPServer, at the call site just above, already writes a
// user-scope registration — but only for the OVERSEER's provider. The daily
// overseer is Grok, so on the daily universe the Claude branch never ran, and
// jevonsmcp reached ~/.claude.json only where a human had once typed
// `claude mcp add` inside a repo: local scope, one directory. Every Claude
// fleet agent started anywhere else therefore had no jevons_* tools at all,
// and on 2026-08-15 one of them reported that absence as a dead control plane.
//
// Written through mcpscope rather than `claude mcp add -s user` deliberately.
// The vendor path is remove-then-add, which has a window where the entry is
// absent from a file the whole fleet reads, and it rewrites the document on
// every boot; mcpscope adds one key, under a lock, and only when the value is
// actually wrong (🎯T376).
//
// Declared residual: Grok's user scope is ensured only when the overseer is
// Grok, so a Grok worker under a Claude overseer keeps the mirror-image gap.
// It did not cause the incident, and writing a TOML editor here would be a
// bigger change than the one being fixed.
func ensureFleetMCPUserScope(cfg config.Config, host string, port int) {
	action, err := ensureFleetMCPUserScopeAt(cfg, host, port, mcpscope.ConfigPath())
	url := fleetMCPURL(host, port)
	switch {
	case err != nil:
		slog.Warn("could not register the fleet MCP server in Claude user scope — agents outside the jevons repo may start without jevons tools",
			"name", fleetMCPName(cfg), "url", url, "err", err)
	case action == fleetMCPSkipped:
		slog.Debug("not the daily universe; leaving the user-scope MCP registration alone",
			"state_dir", cfg.StateDir)
	case action == fleetMCPRepaired:
		slog.Info("repaired the fleet MCP registration: it is now user-scoped, so fleet control follows the agent rather than the directory it starts in",
			"name", fleetMCPName(cfg), "url", url)
	default:
		slog.Info("fleet MCP registration is user-scoped and current",
			"name", fleetMCPName(cfg), "url", url)
	}
}

// ensureFleetMCPUserScopeAt is the decision, with the config path as an
// argument so an oracle can drive it against a fixture instead of the owner's
// real ~/.claude.json.
func ensureFleetMCPUserScopeAt(cfg config.Config, host string, port int, configPath string) (fleetMCPAction, error) {
	// Only the daily universe owns the user-scope entry. A journey isolate
	// serves a throwaway port under its own MCP name and tears that name down
	// through the provider CLI; letting it write here would leave a user-scope
	// registration pointing at a port nothing serves — the silent-dead-
	// registration fault of 🎯T379, reintroduced by the fix for 🎯T464. State
	// dir is the honest discriminator: the isolate asserts its own is outside
	// ~/.jevons, and the daily daemon is the only process for which that is
	// false.
	if !isDailyStateDir(cfg.StateDir) {
		return fleetMCPSkipped, nil
	}
	if configPath == "" {
		return fleetMCPSkipped, mcpscope.ErrNoConfigPath
	}
	changed, err := mcpscope.WriteEnsure(configPath, fleetMCPName(cfg), mcpscope.HTTPEntry(fleetMCPURL(host, port)))
	if err != nil {
		return fleetMCPSkipped, err
	}
	if changed {
		return fleetMCPRepaired, nil
	}
	return fleetMCPCurrent, nil
}

// fleetMCPName is the registration name this daemon serves under. It follows
// cfg.MCPServerName so an isolate cannot collide with the daily entry even if
// the guard above were ever relaxed.
func fleetMCPName(cfg config.Config) string {
	if cfg.MCPServerName == "" {
		return mcpscope.ServerName
	}
	return cfg.MCPServerName
}

// fleetMCPURL is the endpoint as agents will dial it. port is the SERVED port,
// never the configured one, for the 🎯T379 reason its call site gives.
func fleetMCPURL(host string, port int) string {
	return fmt.Sprintf("http://%s:%d/mcp", host, port)
}

// isDailyStateDir reports whether dir is the default ~/.jevons, resolved so a
// relative path or a symlinked home cannot make an isolate look daily.
func isDailyStateDir(dir string) bool {
	if dir == "" {
		return false
	}
	want, err := filepath.Abs(config.Default().StateDir)
	if err != nil {
		return false
	}
	got, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(want); err == nil {
		want = resolved
	}
	if resolved, err := filepath.EvalSymlinks(got); err == nil {
		got = resolved
	}
	return got == want
}
