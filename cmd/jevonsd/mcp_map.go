// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/butler"
	"github.com/marcelocantos/jevons/internal/server"
)

// mcpMapFingerprint reduces an inventory to what the daemon actually uses —
// name, transport, endpoint or command — so the owner's ~/.claude.json,
// which Claude Code rewrites for its own state many times an hour, only
// counts as a config change when the MCP servers in it change. skipNames
// drops session-scoped entries (jevonsmcp) that boot scrub toggles.
func mcpMapFingerprint(inv *claudia.MCPInventory, skipNames map[string]bool) string {
	if inv == nil {
		return ""
	}
	rows := make([]string, 0, len(inv.Servers))
	for _, s := range inv.Servers {
		if skipNames != nil && skipNames[s.Name] {
			continue
		}
		rows = append(rows, mcpServerRow(s))
	}
	sort.Strings(rows)
	return strings.Join(rows, "\n")
}

func mcpServerRow(s claudia.MCPServer) string {
	return s.Name + "\x00" + s.Type + "\x00" + s.URL + "\x00" + s.Command + "\x00" + strings.Join(s.Args, " ")
}

// mcpHTTPUpstreamFingerprint is the subset the loopback HTTP proxy mounts.
func mcpHTTPUpstreamFingerprint(inv *claudia.MCPInventory, skipNames map[string]bool) string {
	if inv == nil {
		return ""
	}
	rows := make([]string, 0, len(inv.Servers))
	for _, s := range inv.Servers {
		if skipNames != nil && skipNames[s.Name] {
			continue
		}
		if strings.TrimSpace(s.URL) == "" {
			continue
		}
		rows = append(rows, s.Name+"\x00"+s.Type+"\x00"+s.URL)
	}
	sort.Strings(rows)
	return strings.Join(rows, "\n")
}

// mcpMapChange summarizes owner-map edits for logs and owner notices (🎯T1133).
type mcpMapChange struct {
	Added   []string
	Removed []string
	Changed []string
}

func mcpMapDiff(prev, next *claudia.MCPInventory, skipNames map[string]bool) mcpMapChange {
	prevBy := mcpServersByName(prev, skipNames)
	nextBy := mcpServersByName(next, skipNames)
	var ch mcpMapChange
	for name, row := range nextBy {
		if old, ok := prevBy[name]; !ok {
			ch.Added = append(ch.Added, name)
		} else if mcpServerRow(old) != mcpServerRow(row) {
			ch.Changed = append(ch.Changed, name)
		}
	}
	for name := range prevBy {
		if _, ok := nextBy[name]; !ok {
			ch.Removed = append(ch.Removed, name)
		}
	}
	sort.Strings(ch.Added)
	sort.Strings(ch.Removed)
	sort.Strings(ch.Changed)
	return ch
}

func mcpServersByName(inv *claudia.MCPInventory, skipNames map[string]bool) map[string]claudia.MCPServer {
	out := map[string]claudia.MCPServer{}
	if inv == nil {
		return out
	}
	for _, s := range inv.Servers {
		if s.Name == "" {
			continue
		}
		if skipNames != nil && skipNames[s.Name] {
			continue
		}
		out[s.Name] = s
	}
	return out
}

func (c mcpMapChange) empty() bool {
	return len(c.Added) == 0 && len(c.Removed) == 0 && len(c.Changed) == 0
}

func (c mcpMapChange) summary() string {
	var parts []string
	if len(c.Added) > 0 {
		parts = append(parts, "added "+strings.Join(c.Added, ", "))
	}
	if len(c.Removed) > 0 {
		parts = append(parts, "removed "+strings.Join(c.Removed, ", "))
	}
	if len(c.Changed) > 0 {
		parts = append(parts, "changed "+strings.Join(c.Changed, ", "))
	}
	return strings.Join(parts, "; ")
}

func notifyMCPOwnerMapChange(srv *server.Server, path string, ch mcpMapChange) {
	if ch.empty() {
		return
	}
	msg := fmt.Sprintf("config reload: %s owner MCP map %s", path, ch.summary())
	slog.Info(msg, "path", path, "added", ch.Added, "removed", ch.Removed, "changed", ch.Changed)
	if srv == nil {
		return
	}
	srv.Broadcast(map[string]any{"type": "capacity_notice", "message": msg})
	srv.LogEvent(configEventComponent, "mcp_owner_map_change", map[string]any{
		"path": path, "added": ch.Added, "removed": ch.Removed, "changed": ch.Changed,
	})
	if err := srv.DeliverToOverseerAs(butler.FormatEventPush(configEventComponent, msg), "agent"); err != nil {
		slog.Warn("mcp owner map reload: overseer notice deliver failed", "err", err)
	}
}
