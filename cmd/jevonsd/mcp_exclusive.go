// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"log/slog"
	"os"
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
