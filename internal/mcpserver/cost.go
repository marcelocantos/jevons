// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/cost"
)

// SetCostMonitor wires the budget monitor and registers the cost tool —
// the Go-native answer to "what is burning right now" that the
// 2026-07-06 incident took an hour of manual ps/lsof/mnemo to produce.
func (s *Server) SetCostMonitor(snapshot func() (*cost.Snapshot, error)) {
	s.costSnapshot = snapshot

	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_cost",
			mcp.WithDescription("What is burning tokens right now: rolling USD/hr burn-rate globally, for the jevons fleet, and per worker; today's spend and end-of-day projection; per-session burn (hottest first); and any tripped runaway signals (rate ceilings, session-count bound, orphan sessions, projected/hard-ceiling overspend). Use this to answer cost questions instead of manual ps/lsof."),
		),
		s.handleCost,
	)
}

func (s *Server) handleCost(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.costSnapshot == nil {
		return mcp.NewToolResultError("cost monitoring is not enabled"), nil
	}
	snap, err := s.costSnapshot()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Burn rate (last %s window):\n", snap.Window)
	fmt.Fprintf(&b, "  global: $%.2f/hr   fleet: $%.2f/hr\n", snap.GlobalUSDPerHour, snap.FleetUSDPerHour)
	fmt.Fprintf(&b, "  today: $%.2f spent, $%.2f projected\n", snap.SpentTodayUSD, snap.ProjectedTodayUSD)
	if len(snap.WorkerUSDPerHour) > 0 {
		b.WriteString("  per worker:")
		for w, r := range snap.WorkerUSDPerHour {
			fmt.Fprintf(&b, " %s=$%.2f/hr", w, r)
		}
		b.WriteByte('\n')
	}
	if len(snap.Sessions) > 0 {
		b.WriteString("\nWhat is burning (hottest first):\n")
		for i, r := range snap.Sessions {
			if i >= 10 {
				fmt.Fprintf(&b, "  … and %d more\n", len(snap.Sessions)-10)
				break
			}
			who := r.Worker
			if who == "" {
				who = "(unattributed)"
			}
			fmt.Fprintf(&b, "  $%.2f  %s  %s  %s\n", r.CostUSD, r.SessionID[:min(8, len(r.SessionID))], who, r.Model)
		}
	}
	if len(snap.Alerts) > 0 {
		b.WriteString("\n⚠ RUNAWAY SIGNALS:\n")
		for _, a := range snap.Alerts {
			fmt.Fprintf(&b, "  [%s] %s: %s\n", a.Level, a.Kind, a.Detail)
		}
	} else {
		b.WriteString("\nNo runaway signals.\n")
	}
	return mcp.NewToolResultText(b.String()), nil
}

// CostJSON returns the snapshot as a JSON-serialisable value for the web
// /api/cost endpoint (nil-safe).
func (s *Server) CostJSON() any {
	if s.costSnapshot == nil {
		return nil
	}
	snap, err := s.costSnapshot()
	if err != nil {
		return map[string]string{"error": err.Error()}
	}
	// Round-trip through the tagged struct so the API shape is stable.
	raw, _ := json.Marshal(snap)
	var out any
	_ = json.Unmarshal(raw, &out)
	return out
}
