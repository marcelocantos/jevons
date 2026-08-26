// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/planusage"
)

// SetPlanUsageSource wires GET /api/plan-usage into jevons_plan_usage
// (🎯T390.1.4) so the overseer can read the header ticker without curling
// the cockpit. Nil leaves the tool unregistered.
func (s *Server) SetPlanUsageSource(snapshot func() planusage.Snapshot) {
	s.planUsage = snapshot
	if snapshot == nil {
		return
	}
	s.addTool(
		mcp.NewTool("jevons_plan_usage",
			mcp.WithDescription("Subscription remaining as painted in the cockpit header ticker (🎯T390 / T390.1.4): per-provider session and weekly remaining percent, 429/rate_limit as exhausted 0% (not unpublished), unpublished Grok as unavailable with the reason, idle Bedrock omitted. Use this to decide where to put the next job. Distinct from jevons_cost (USD burn) and jevons_capacity_status (admission)."),
		),
		s.handlePlanUsage,
	)
}

func (s *Server) handlePlanUsage(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.planUsage == nil {
		return mcp.NewToolResultError("plan usage is not configured"), nil
	}
	return mcp.NewToolResultText(planusage.FormatCockpit(s.planUsage())), nil
}
