// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"maps"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/capacity"
	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/hostload"
)

// SetCapacityGovernor attaches the background-work admission governor
// (🎯T359) and registers jevons_capacity_status. The governor decides which
// ambient cycles run under the current budget and load; it never spends or
// clamps anything itself — the 🎯T36 enforcer remains the safety net.
func (s *Server) SetCapacityGovernor(g *capacity.Governor) {
	s.mu.Lock()
	s.capacityGov = g
	s.mu.Unlock()
	if g == nil {
		return
	}
	s.addTool(
		mcp.NewTool("jevons_capacity_status",
			mcp.WithDescription("Show background-work admission control (🎯T359): current capacity pressure, remaining headroom by dimension (cost / tokens / load), which background classes are running, and what each class would be granted right now. Owner turns and open Build missions always outrank ambient background (research, audits, coach, sentinel extras). Use this to explain why an ambient cycle did not run."),
		),
		s.handleCapacityStatus,
	)
}

// CapacityGovernor returns the attached governor (may be nil).
func (s *Server) CapacityGovernor() *capacity.Governor {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.capacityGov
}

// HarnessLoad exposes the per-provider registered-agent counts that feed both
// portfolio routing (🎯T325.2) and capacity load headroom (🎯T359).
func (s *Server) HarnessLoad() map[string]int {
	return map[string]int(s.harnessLoadCounts())
}

// EffectiveProviderSoftCaps returns the portfolio's soft caps after
// budget.json overlays — the denominator of provider load headroom.
func (s *Server) EffectiveProviderSoftCaps() map[string]int {
	p := s.effectivePortfolio()
	if p == nil {
		return nil
	}
	out := make(map[string]int, len(p.SoftCaps))
	maps.Copy(out, p.SoftCaps)
	return out
}

// FleetAgentCount returns the number of registered fleet agents — the
// "concurrent load" the owner sees in the RHS panel.
func (s *Server) FleetAgentCount() int {
	if s == nil || s.registry == nil {
		return 0
	}
	return len(s.registry.List())
}

func (s *Server) handleCapacityStatus(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	gov := s.CapacityGovernor()
	if gov == nil {
		return mcp.NewToolResultError("capacity admission control not configured"), nil
	}
	st := gov.Status()

	var b strings.Builder
	fmt.Fprintf(&b, "Background capacity (🎯T359) at %s\n", st.At)
	fmt.Fprintf(&b, "  pressure: %s (headroom %.0f%%)\n", st.Assessment.Pressure, st.Assessment.Headroom*100)
	fmt.Fprintf(&b, "  headroom: cost=%s tokens=%s load=%s plan=%s\n",
		headroomText(st.Assessment.CostHeadroom),
		headroomText(st.Assessment.TokenHeadroom),
		headroomText(st.Assessment.LoadHeadroom),
		headroomText(st.Assessment.PlanHeadroom))
	b.WriteString(hostLoadText(st.Snapshot))
	if d := capacity.AdmitSpawn(capacity.SpawnWorker, st.Snapshot, st.Policy); !d.Admitted() {
		fmt.Fprintf(&b, "  spawn: refuse new worker panes (%s) (🎯T460)\n", d.Reason)
	} else {
		b.WriteString("  spawn: new worker panes admitted (🎯T460)\n")
	}
	if st.Snapshot.PlanSource != "" {
		fmt.Fprintf(&b, "  plan source: %s (🎯T390)\n", st.Snapshot.PlanSource)
	}
	if st.Snapshot.Accounting != "" {
		fmt.Fprintf(&b, "  accounting: %s (billable=%v)\n", st.Snapshot.Accounting, st.Snapshot.Billable)
	}
	for _, r := range st.Assessment.Reasons {
		fmt.Fprintf(&b, "  reason: %s\n", r)
	}
	if st.Assessment.Residual != "" {
		fmt.Fprintf(&b, "  residual: %s\n", st.Assessment.Residual)
	}

	b.WriteString("  classes (highest priority first):\n")
	for _, row := range st.Classes {
		if row.Running == 0 && row.Admitted == 0 && row.Deferred == 0 && row.Last == nil {
			continue
		}
		fmt.Fprintf(&b, "    %-15s running=%d admitted=%d deferred=%d", row.Class, row.Running, row.Admitted, row.Deferred)
		if row.Last != nil {
			fmt.Fprintf(&b, " last=%s/%s", row.Last.Verdict, row.Last.Reason)
		}
		b.WriteString("\n")
	}
	b.WriteString("  if it asked now:\n")
	for _, d := range st.Plan {
		tier := d.Tier
		if tier == "" {
			tier = "-"
		}
		fmt.Fprintf(&b, "    %-15s %s (%s, tier=%s) %s\n", d.Class, d.Verdict, d.Reason, tier, d.Detail)
	}
	return mcp.NewToolResultText(b.String()), nil
}

// headroomText renders a headroom fraction, naming the unknown case rather
// than printing a fabricated -100%.
func headroomText(f float64) string {
	if f < 0 {
		return "unknown"
	}
	return fmt.Sprintf("%.0f%%", f*100)
}

// CapacitySnapshotArgs are the live sources the daemon wires into a capacity
// snapshot. Every field is optional: a missing source contributes no pressure
// rather than a guessed one.
type CapacitySnapshotArgs struct {
	// Cost is the 🎯T36 monitor snapshot function (nil when the cost spine
	// is unavailable or disabled).
	Cost func() (*cost.Snapshot, error)
	// Budget is the current budget config (session bound, daily lines).
	Budget func() *cost.BudgetConfig
	// TokensToday totals tokens consumed since local midnight; DailyTokens
	// is the owner's daily token budget (0 = unset, which is honest: token
	// headroom then reports unknown rather than inventing a ceiling).
	TokensToday func() int64
	DailyTokens func() int64
	// SpawnHalted reports the 🎯T36 clamp state (typically enforcer.AllowSpawn).
	SpawnHalted func() bool
	// OwnerActive reports an owner turn in flight (🎯T291).
	OwnerActive func() bool
	// ProviderLoad / ProviderSoftCaps are the 🎯T325.2 portfolio spread.
	ProviderLoad     func() map[string]int
	ProviderSoftCaps func() map[string]int
	// PlanRemaining is the 🎯T390 subscription-plan lever: the tightest
	// published remaining fraction across backends the fleet is running on,
	// and what produced it. A nil fraction means no running backend publishes
	// remaining — which stays unknown rather than becoming a confident 100%.
	PlanRemaining func() (*float64, string)
	// HostLoad reads the host's own saturation — run-queue length per core and
	// swap occupancy (🎯T463). It is the dimension that runs out first under
	// fan-out, and the one admission was blind to on 2026-08-15.
	HostLoad func() hostload.Sample
}

// CapacitySnapshot builds one capacity snapshot from live sources. It is the
// single adapter between the cost/portfolio subsystems and the pure policy,
// so the policy package stays dependency-free and hermetically testable.
func CapacitySnapshot(args CapacitySnapshotArgs) capacity.Snapshot {
	snap := capacity.Snapshot{}

	if args.Budget != nil {
		if cfg := args.Budget(); cfg != nil {
			snap.Accounting = cfg.EffectiveAccounting()
			snap.Billable = snap.Accounting == cost.AccountingListPrice
			snap.DailyBudgetUSD = cfg.DailyBudgetUSD
			snap.HardCeilingUSDPerDay = cfg.HardCeilingUSDPerDay
			snap.MaxSessions = cfg.MaxSessions
		}
	}
	if args.Cost != nil {
		if cs, err := args.Cost(); err == nil && cs != nil {
			snap.Accounting = cs.Accounting
			snap.Billable = cs.Billable
			snap.SpentTodayUSD = cs.SpentTodayUSD
			snap.ProjectedTodayUSD = cs.ProjectedTodayUSD
			snap.ActiveSessions = len(cs.Sessions)
			snap.HighestAlert = highestAlertLevel(cs.Alerts)
		}
	}
	if args.TokensToday != nil {
		snap.TokensUsed = args.TokensToday()
	}
	if args.DailyTokens != nil {
		snap.TokensBudget = args.DailyTokens()
	}
	if args.SpawnHalted != nil {
		snap.SpawnHalted = args.SpawnHalted()
	}
	if args.OwnerActive != nil {
		snap.OwnerActive = args.OwnerActive()
	}
	if args.ProviderLoad != nil {
		snap.ProviderLoad = args.ProviderLoad()
	}
	if args.ProviderSoftCaps != nil {
		snap.ProviderSoftCaps = args.ProviderSoftCaps()
	}
	if args.PlanRemaining != nil {
		snap.PlanRemaining, snap.PlanSource = args.PlanRemaining()
	}
	if args.HostLoad != nil {
		applyHostLoad(&snap, args.HostLoad())
	}
	return snap
}

// highestAlertLevel returns the most severe alert level in the snapshot.
func highestAlertLevel(alerts []cost.Alert) string {
	top := cost.LevelNone
	for _, a := range alerts {
		if a.Level > top {
			top = a.Level
		}
	}
	return top.String()
}
