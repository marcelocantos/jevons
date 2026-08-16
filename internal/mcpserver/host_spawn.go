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
