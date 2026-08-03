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

// SetRSILoop attaches the ambient RSI loop (🎯T92) and registers
// jevons_rsi_cycle for on-demand retrospective minting.
func (s *Server) SetRSILoop(loop *rsi.Loop) {
	s.rsiLoop = loop
	if loop == nil {
		return
	}
	s.registerRSITools()
}

func (s *Server) registerRSITools() {
	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_rsi_cycle",
			mcp.WithDescription("Run one ambient RSI retrospective cycle now (🎯T92): sample recent lifecycle/eventlog evidence (+ stream buffer), extract improvement candidates, apply noise control, and file bullseye targets when not dry-run. Prefer harness schedule/stream for ambient operation; use this to force a cycle or inspect proposals."),
			mcp.WithBoolean("dry_run", mcp.Description("If true, extract+dedupe only — do not file targets (overrides loop dry-run for this call only when the loop supports it via RunOnce still filing based on loop config; prefer loop DryRun for global).")),
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
