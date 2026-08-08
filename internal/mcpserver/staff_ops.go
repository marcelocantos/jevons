// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/butler"
	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/staffops"
)

// staffOpsState holds in-memory cooldown for re-file of the same symptom
// (🎯T325.4 one-shot; shared with durable sentinel 🎯T219 when both run).
type staffOpsState struct {
	mu       sync.Mutex
	cooldown staffops.Cooldown
}

// registerStaffOpsTools exposes one bounded staff ops cycle (🎯T325.4):
// sample health + resource snapshot → pure policy → deliver to root.
// Not a permanent monologue; continuous loop is jevons_sentinel_cycle / StartSentinelLoop (T219).
func (s *Server) registerStaffOpsTools() {
	if s.staffOps == nil {
		s.staffOps = &staffOpsState{
			cooldown: staffops.Cooldown{
				LastFile: make(map[string]time.Time),
				Duration: staffops.DefaultCooldown,
			},
		}
	}
	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_staff_ops_cycle",
			mcp.WithDescription("Run one bounded staff ops cycle (🎯T325.4): sample health-of-health (fleet/dead agents, cost alerts) + compact resource snapshot (sessions, burn, agent counts, idle PO heuristic), classify harness-ok|repair|file+PO|ignore with cooldown on re-file, deliver compact brief to root overseer. Not permanent monologue; does not implement product code or open Ship. Continuous sentinel is jevons_sentinel_cycle (🎯T219)."),
			mcp.WithBoolean("dry_run", mcp.Description("If true, build classification and wire text without delivering to overseer and without updating cooldown.")),
			mcp.WithNumber("frontier_depth", mcp.Description("Optional frontier leaf count when caller already knows it (default 0 = unknown/not sampled).")),
			mcp.WithString("overseer", mcp.Description("Deliver target agent name (default jevons).")),
		),
		s.handleStaffOpsCycle,
	)
}

func (s *Server) handleStaffOpsCycle(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	dryRun := false
	if v, ok := args["dry_run"].(bool); ok {
		dryRun = v
	}
	frontierDepth := 0
	if v, ok := args["frontier_depth"].(float64); ok && v > 0 {
		frontierDepth = int(v)
	}
	overseer := s.overseerName()
	if v, ok := args["overseer"].(string); ok && strings.TrimSpace(v) != "" {
		overseer = strings.TrimSpace(v)
	}

	signals, resources := s.sampleStaffOps(frontierDepth)

	s.mu.Lock()
	st := s.staffOps
	s.mu.Unlock()
	if st == nil {
		st = &staffOpsState{
			cooldown: staffops.Cooldown{
				LastFile: make(map[string]time.Time),
				Duration: staffops.DefaultCooldown,
			},
		}
		s.mu.Lock()
		s.staffOps = st
		s.mu.Unlock()
	}

	st.mu.Lock()
	res := staffops.RunCycle(staffops.CycleArgs{
		Signals:   signals,
		Resources: resources,
		Cooldown:  &st.cooldown,
		Now:       time.Now().UTC(),
		DryRun:    dryRun,
	})
	st.mu.Unlock()

	delivered := false
	if !dryRun {
		if err := s.deliverStaffOpsReport(overseer, res.WireText); err != nil {
			s.logLifecycle("staff_ops", "cycle", "error", map[string]any{
				"err": err.Error(), "primary": string(res.Primary),
			})
			return mcp.NewToolResultError(fmt.Sprintf("staff ops cycle: classify ok but deliver failed: %v\n\n%s", err, res.WireText)), nil
		}
		delivered = true
	}

	s.logLifecycle("staff_ops", "cycle", "ok", map[string]any{
		"primary":   string(res.Primary),
		"signals":   len(res.Decisions),
		"filed":     len(res.FiledSymptoms),
		"dry_run":   dryRun,
		"delivered": delivered,
	})

	var b strings.Builder
	fmt.Fprintf(&b, "Staff ops cycle: primary=%s signals=%d filed=%d dry_run=%v delivered=%v\n\n",
		res.Primary, len(res.Decisions), len(res.FiledSymptoms), dryRun, delivered)
	b.WriteString(res.WireText)
	return mcp.NewToolResultText(b.String()), nil
}

// sampleStaffOps builds pure inputs from registry + cost monitor (no product implement).
func (s *Server) sampleStaffOps(frontierDepth int) ([]staffops.Signal, staffops.ResourceSnapshot) {
	var signals []staffops.Signal
	resources := staffops.ResourceSnapshot{FrontierDepth: frontierDepth}

	// Fleet sample.
	if s.registry != nil {
		overseer := s.overseerName()
		// Dead-handle recovery (T85 mechanical floor).
		reps := SweepDeadAgents(s.registry, overseer)
		for _, r := range reps {
			sig := staffops.Signal{
				Kind:         "dead_agent",
				Symptom:      "dead:" + r.Name,
				Severity:     "high",
				Mechanical:   true,
				HarnessActed: r.Recovered,
				// Thin vertical: treat detection as grace already elapsed so
				// unrecovered dead agents classify as repair (T219 spirit).
				GraceElapsed: !r.Recovered,
				Detail:       FormatDeadAgentReport([]DeadAgentReport{r}),
			}
			if r.Error != "" {
				sig.Detail = r.Error
			}
			signals = append(signals, sig)
		}

		running, stopped, idlePO := 0, 0, 0
		for _, d := range s.registry.List() {
			if d.Name == "" || d.Name == overseer {
				continue
			}
			proc := s.registry.Get(d.Name)
			alive := proc != nil && proc.Alive()
			if alive {
				running++
			} else {
				stopped++
				if isPOName(d.Name) {
					idlePO++
				}
			}
		}
		resources.RunningAgents = running
		resources.StoppedAgents = stopped
		resources.IdlePOCount = idlePO
	} else {
		resources.Note = "registry unavailable"
	}

	// Cost / load sample (T137).
	s.mu.Lock()
	snapFn := s.costSnapshot
	s.mu.Unlock()
	if snapFn != nil {
		if snap, err := snapFn(); err == nil && snap != nil {
			resources.SessionCount = len(snap.Sessions)
			resources.GlobalUSDPerHour = snap.GlobalUSDPerHour
			resources.FleetUSDPerHour = snap.FleetUSDPerHour
			resources.SpentTodayUSD = snap.SpentTodayUSD
			resources.Accounting = snap.Accounting
			for _, a := range snap.Alerts {
				sev := "medium"
				if a.Level >= cost.LevelPause {
					sev = "critical"
				}
				signals = append(signals, staffops.Signal{
					Kind:       "cost_alert",
					Symptom:    "cost:" + a.Kind,
					Severity:   sev,
					Mechanical: false, // residual policy: cost thrash → root file path
					Detail:     a.Detail,
				})
			}
		} else if err != nil {
			if resources.Note != "" {
				resources.Note += "; "
			}
			resources.Note += "cost snapshot error"
		}
	} else {
		if resources.Note != "" {
			resources.Note += "; "
		}
		resources.Note += "cost monitor off"
	}

	return signals, resources
}

// isPOName is a thin heuristic for idle PO count (name ends with -po or is *po*).
// Not ledger membership — residual: path portfolios (T200) for richer counts.
func isPOName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return false
	}
	if strings.HasSuffix(n, "-po") || strings.HasSuffix(n, "_po") {
		return true
	}
	// e.g. jevons-po already matched; also plain "po" role names.
	return n == "po" || strings.Contains(n, "-po-")
}

func (s *Server) deliverStaffOpsReport(overseer, text string) error {
	if strings.TrimSpace(overseer) == "" {
		overseer = "jevons"
	}
	body := butler.FormatEventPush(staffops.EventSource, text)
	// Fire-and-forget: queue if busy (same lesson as RSI coach).
	if _, err := s.sendToAgent(overseer, body, false); err != nil {
		if s.butler != nil {
			if _, err2 := s.butler.PushEvent(overseer, staffops.EventSource, text); err2 != nil {
				return fmt.Errorf("staff ops deliver send: %w; push: %v", err, err2)
			}
			slog.Info("staff ops delivered via push fallback", "target", overseer)
			return nil
		}
		// Last resort: notify wire (owner-adjacent but still root channel when
		// registry send unavailable in tests).
		if s.notifyJevon != nil {
			s.notifyJevon(body)
			return nil
		}
		return err
	}
	slog.Info("staff ops delivered cycle", "target", overseer)
	return nil
}
