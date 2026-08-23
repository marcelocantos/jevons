// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"log/slog"
	"os"
	"strings"
	"syscall"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/mcpattach"
)

// stampRegistryMCPExclusive flips MCPExclusive on every registry row that
// still lacks it, and SIGTERMs leftover Grok connect-mode serves so the
// next Launch respawns them with GROK_HOME (claudia exclusive MCP).
func stampRegistryMCPExclusive(reg *claudia.Registry, overseer *claudia.AgentDef) {
	if overseer != nil && mcpattach.StampExclusive(overseer) {
		dropGrokConnect(overseer)
	}
	if reg == nil {
		return
	}
	for _, d := range reg.List() {
		def := d
		if overseer != nil && def.Name == overseer.Name {
			continue
		}
		if def.MCPExclusive {
			continue
		}
		if mcpattach.StampExclusive(&def) {
			dropGrokConnect(&def)
		}
		if err := reg.Register(def); err != nil {
			slog.Warn("could not stamp MCPExclusive", "name", def.Name, "err", err)
		}
	}
}

// stampRegistrySessionMCP restamps MCPServers from the live attach
// (jevonsmcp URL + T520 loopbacks) so leftover exclusive seats do not
// keep remote owner-map URLs after a restart.
func stampRegistrySessionMCP(reg *claudia.Registry, attach mcpattach.Args, overseer *claudia.AgentDef) {
	if overseer != nil {
		overseer.MCPServers = mcpattach.SessionServers(attach, overseer.Provider, overseer.WorkDir)
	}
	if reg == nil || strings.TrimSpace(attach.URL) == "" {
		return
	}
	for _, d := range reg.List() {
		def := d
		if overseer != nil && def.Name == overseer.Name {
			continue
		}
		want := mcpattach.SessionServers(attach, def.Provider, def.WorkDir)
		if mcpattach.ServersEqual(def.MCPServers, want) {
			continue
		}
		def.MCPServers = want
		if err := reg.Register(def); err != nil {
			slog.Warn("could not stamp MCPServers", "name", def.Name, "err", err)
		}
	}
}

func dropGrokConnect(def *claudia.AgentDef) {
	if def == nil {
		return
	}
	if def.ConnectPID > 0 {
		if p, err := os.FindProcess(def.ConnectPID); err == nil {
			if err := p.Signal(syscall.SIGTERM); err != nil {
				slog.Warn("could not stop leftover Grok serve", "pid", def.ConnectPID, "err", err)
			} else {
				slog.Info("stopped leftover Grok serve (MCPExclusive; no GROK_HOME)",
					"name", def.Name, "pid", def.ConnectPID)
			}
		}
	}
	def.ConnectURL = ""
	def.ConnectPID = 0
	def.GrokConnect = false
}
