// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/capacity"
)

// checkHostSpawnAllowed refuses jevons_agent_start when the host is at
// critical pressure (🎯T460). Owner seats and control-plane repair pass.
// A missing governor is unknown, not critical — the cost clamp remains
// the T36 safety net.
func (s *Server) checkHostSpawnAllowed(purpose, name string) *mcp.CallToolResult {
	if s == nil {
		return nil
	}
	gov := s.CapacityGovernor()
	if gov == nil {
		return nil
	}
	kind := capacity.ClassifySpawnKind(purpose, name)
	d := gov.AdmitSpawn(kind, name)
	if d.Admitted() {
		return nil
	}
	return mcp.NewToolResultError(fmt.Sprintf(
		"refusing to start %q — host pressure %s (%s): %s. Owner turns and control-plane repair are not blocked (🎯T460).",
		name, d.Pressure, d.Reason, d.Detail))
}

// checkDestSpawnAllowed is the dest-bound seat check (🎯T715). Call after
// mintProviderPick so an omit-provider mint on grok is not refused because
// claude is at cap. An explicit dest that is at cap is still refused, and
// the refusal names dests that have headroom.
func (s *Server) checkDestSpawnAllowed(purpose, name, dest string) *mcp.CallToolResult {
	if s == nil {
		return nil
	}
	gov := s.CapacityGovernor()
	if gov == nil {
		return nil
	}
	kind := capacity.ClassifySpawnKind(purpose, name)
	d := gov.AdmitSpawnDest(kind, name, dest)
	if d.Admitted() {
		return nil
	}
	return mcp.NewToolResultError(fmt.Sprintf(
		"refusing to start %q — host pressure %s (%s): %s. Owner turns and control-plane repair are not blocked (🎯T460).",
		name, d.Pressure, d.Reason, d.Detail))
}

// admitRehydrate is checkHostSpawnAllowed for the *rehydrate* path (🎯T1112):
// a send/repressure that finds a registered seat stopped is about to mint a
// process exactly as jevons_agent_start does, and the acceptance is that a
// rehydrate is admitted through the same spawn governor a fresh mint is —
// seat-count, memory grind (🎯T566.2), capacity (🎯T359) — not waved through
// because the handle already existed. purpose is the registry row's own
// purpose (empty reads as work, same as ClassifySpawnKind).
func (s *Server) admitRehydrate(purpose, name string) error {
	if s == nil {
		return nil
	}
	gov := s.CapacityGovernor()
	if gov == nil {
		return nil
	}
	kind := capacity.ClassifySpawnKind(purpose, name)
	d := gov.AdmitSpawn(kind, name)
	if d.Admitted() {
		return nil
	}
	return fmt.Errorf("agent %q is not running and cannot be rehydrated right now — host pressure %s (%s): %s (🎯T460/🎯T1112); owner turns and control-plane repair are not blocked",
		name, d.Pressure, d.Reason, d.Detail)
}
