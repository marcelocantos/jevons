// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"log/slog"
	"strings"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/mcpattach"
)

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
