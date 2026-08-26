// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/rsi"
)

// SetRSILoop attaches the residual RSI mint loop (🎯T92) and registers
// jevons_rsi_cycle. Product path is the coach (🎯T243 / jevons_rsi_coach_*);
// mint is opt-in residual and must not be the standing ambient product path.
func (s *Server) SetRSILoop(loop *rsi.Loop) {
	s.rsiLoop = loop
	if loop == nil {
		return
	}
	s.registerRSITools()
}

func (s *Server) registerRSITools() {
	s.addTool(
		mcp.NewTool("jevons_rsi_cycle",
			mcp.WithDescription("RESIDUAL (🎯T92 mint): run one direct bullseye-mint cycle. Product path is jevons_rsi_coach_cycle (🎯T243) — coach posts judgments to overseer; overseer alone files. This tool files only when the residual mint loop is configured (JEVONS_RSI_MINT/DEEPER). Prefer coach for ambient RSI."),
			mcp.WithBoolean("dry_run", mcp.Description("If true, extract+dedupe only — do not file targets (advisory; loop DryRun owns filing).")),
		),
		s.handleRSICycle,
	)
}

func (s *Server) handleRSICycle(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.rsiLoop == nil {
		return mcp.NewToolResultError("ambient RSI loop not configured"), nil
	}
	// dry_run arg is advisory in the response; loop config owns filing.
	// Callers who need a guaranteed dry pass should set JEVONS_RSI_DRY_RUN on the daemon.
	args := req.GetArguments()
	dryHint := false
	if v, ok := args["dry_run"].(bool); ok {
		dryHint = v
	}

	res, err := s.rsiLoop.RunOnce("mcp")
	if err != nil {
		s.logLifecycle("rsi", "cycle", "error", map[string]any{
			"err": err.Error(),
		})
		return mcp.NewToolResultError(fmt.Sprintf("rsi cycle failed: %v", err)), nil
	}
	s.logLifecycle("rsi", "cycle", "ok", map[string]any{
		"proposed": len(res.Proposed),
		"filed":    len(res.Filed),
		"skipped":  len(res.Skipped),
	})

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Ambient RSI cycle: proposed=%d filed=%d skipped=%d\n",
		len(res.Proposed), len(res.Filed), len(res.Skipped)))
	if dryHint && len(res.Filed) > 0 {
		b.WriteString("(note: dry_run arg is a hint; loop filed because daemon dry-run is off)\n")
	}
	for _, f := range res.Filed {
		b.WriteString(fmt.Sprintf("  filed 🎯%s — %s (fp=%s)\n", f.ID, f.Name, f.Fingerprint))
	}
	for _, c := range res.Proposed {
		b.WriteString(fmt.Sprintf("  proposed: %s (count=%d fp=%s)\n", c.Name, c.Count, c.Fingerprint))
	}
	for _, sk := range res.Skipped {
		b.WriteString(fmt.Sprintf("  skipped: %s (%s)\n", sk.Name, sk.Reason))
	}
	return mcp.NewToolResultText(b.String()), nil
}
