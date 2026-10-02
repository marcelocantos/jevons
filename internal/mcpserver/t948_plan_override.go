// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/planusage"
)

// 🎯T948: the owner's band override on a plan. The store is applied to the
// plan snapshot at its source, so the cockpit bars, the plan sweep and the
// mint pick all read the same verdict; this file is the tool that sets it
// and the mint's half.

// SetPlanOverrides registers jevons_plan_override over store.
func (s *Server) SetPlanOverrides(store *planusage.OverrideStore) {
	s.planOverrides = store
	if store == nil {
		return
	}
	s.addTool(
		mcp.NewTool("jevons_plan_override",
			mcp.WithDescription("Owner override on a subscription plan's band (🎯T948). action=set plan=claude reason=\"…\" [band=ok] paints the plan in that band on the cockpit (a ? on its box shows the reason verbatim), keeps every seat on it however hot or exhausted its readings are, and mints new seats on it. The readings are unchanged. action=clear removes it; action=status lists overrides. Only on the owner's word."),
			mcp.WithString("action", mcp.Required(), mcp.Description("set | clear | status")),
			mcp.WithString("plan", mcp.Description("claude, codex, grok or cursor (set / clear).")),
			mcp.WithString("band", mcp.Description("Band to paint and act on: ok (default), under or locked keep seats on the plan and mint new ones there; exhausted keeps every seat off it (🎯T987).")),
			mcp.WithString("reason", mcp.Description("Free text shown on the plan's ? in the cockpit, e.g. 'Owner has a Claude reset available; spend it before other plans.' Required for set.")),
		),
		s.handlePlanOverride,
	)
}

// planOverrideMint is the seat provider of a plan overridden into a dest
// band, or "" when none is.
func (s *Server) planOverrideMint() string {
	if s.planUsage == nil {
		return ""
	}
	var plans []string
	for _, be := range s.planUsage().Backends {
		if be.Override != nil && planusage.IsDestBandOverride(be.Override.Band) {
			plans = append(plans, strings.ToLower(be.Provider))
		}
	}
	if len(plans) == 0 {
		return ""
	}
	sort.Strings(plans)
	return string(claudia.SubscriptionSeatProvider(claudia.Provider(plans[0])))
}

func (s *Server) handlePlanOverride(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	store := s.planOverrides
	plan := strings.ToLower(strings.TrimSpace(req.GetString("plan", "")))
	switch strings.ToLower(strings.TrimSpace(req.GetString("action", ""))) {
	case "status":
		m, err := store.Load()
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if len(m) == 0 {
			return mcp.NewToolResultText("no plan overrides: every plan is judged on its readings"), nil
		}
		var b strings.Builder
		for p, ov := range m {
			fmt.Fprintf(&b, "%s: %s since %s — %s\n", p, ov.Band, ov.SetAt.Format(time.RFC3339), ov.Reason)
		}
		return mcp.NewToolResultText(b.String()), nil
	case "clear":
		if plan == "" {
			return mcp.NewToolResultError("plan is required"), nil
		}
		if err := store.Clear(plan); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(plan + " override cleared: it is judged on its readings again"), nil
	case "set":
		switch plan {
		case "claude", "codex", "grok", "cursor":
		default:
			return mcp.NewToolResultError("plan must be claude, codex, grok or cursor"), nil
		}
		reason := strings.TrimSpace(req.GetString("reason", ""))
		if reason == "" {
			return mcp.NewToolResultError("reason is required: it is what the cockpit shows on the plan's ?"), nil
		}
		band := planusage.WeeklyBand(strings.ToLower(strings.TrimSpace(req.GetString("band", string(planusage.BandOK)))))
		if !planusage.IsDestBandOverride(band) && !planusage.IsKeepOffBandOverride(band) {
			return mcp.NewToolResultError("band must be ok, under or locked (seat on it) or exhausted (keep seats off it)"), nil
		}
		if err := store.Set(plan, planusage.Override{Band: band, Reason: reason, SetBy: "owner", SetAt: time.Now()}); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if planusage.IsKeepOffBandOverride(band) {
			// 🎯T987: the keep-off half — no new seat resolves onto it
			// whatever its readings say, and running seats leave.
			return mcp.NewToolResultText(fmt.Sprintf("%s overridden to %s: no new seat is minted or migrated onto it, whatever its readings say. Reason shown: %s", plan, band, reason)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("%s overridden to %s: seats stay on it and new seats mint on it. Reason shown: %s", plan, band, reason)), nil
	default:
		return mcp.NewToolResultError("action must be set, clear or status"), nil
	}
}
