// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/planusage"
)

// 🎯T561: jevons_agent_migrate off a Claude seat with weekly remaining is
// refused with kill+start advice; exhausted Claude or owner_asked passes.
func TestT561MigrateRefusesLeavingClaudeWithWeeklyRemaining(t *testing.T) {
	reg := t561Registry(t, "jevons-po")
	pct := func(v float64) *float64 { return &v }
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	week := now.Add(4 * 24 * time.Hour)
	lim := planusage.DefaultWeeklyWindowSeconds
	snap := func(rem, used float64) func() planusage.Snapshot {
		return func() planusage.Snapshot {
			return planusage.Snapshot{At: now, Backends: []planusage.Backend{{
				Provider: "claude", Status: planusage.StatusAvailable, Windows: []planusage.Window{{
					Name: planusage.WindowWeekly, RemainingPercent: pct(rem), UsedPercent: pct(used),
					ResetsAt: &week, LimitWindowSeconds: &lim,
				}}}}}
		}
	}
	call := func(s *Server, ownerAsked bool) *mcp.CallToolResult {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{"name": "jevons-po", "provider": "cursor", "owner_asked": ownerAsked}
		res, err := s.handleAgentMigrate(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	s := &Server{registry: reg, migrator: nil, planUsage: snap(60, 40)}
	// migrator nil would refuse first; give the T561 gate precedence by
	// checking its text directly.
	if msg := s.refuseContextRemintMigrate("jevons-po", "cursor", false); !strings.Contains(msg, "jevons_agent_kill(jevons-po)") || !strings.Contains(msg, `provider="claude"`) {
		t.Fatalf("weekly remaining → refused with kill+start advice, got %q", msg)
	}
	if msg := s.refuseContextRemintMigrate("jevons-po", "cursor", true); msg != "" {
		t.Fatalf("owner_asked → allowed, got %q", msg)
	}
	s.planUsage = snap(0, 100)
	if msg := s.refuseContextRemintMigrate("jevons-po", "cursor", false); msg != "" {
		t.Fatalf("claude exhausted → allowed, got %q", msg)
	}
	s.planUsage = nil
	if msg := s.refuseContextRemintMigrate("jevons-po", "cursor", false); !strings.Contains(msg, "unknown is not exhausted") {
		t.Fatalf("no plan feed → stay on claude, got %q", msg)
	}
	// Through the tool handler the refusal lands as a tool error.
	s.planUsage = snap(60, 40)
	if res := call(s, false); !res.IsError || !strings.Contains(toolText(res), "🎯T561") {
		t.Fatalf("tool did not refuse: %s", toolText(res))
	}
}

// 🎯T561: the T417 unworkable notice carries the remint advice.
func TestT561UnworkableNoticeCarriesRemintAdvice(t *testing.T) {
	reg := t561Registry(t, "jv-x")
	s := &Server{registry: reg}
	plan, ok := s.contextRemintPlan("jv-x", false)
	if !ok || plan.Mode != planusage.RemintSameProvider {
		t.Fatalf("plan = %+v ok=%v", plan, ok)
	}
	if !strings.Contains(plan.Advice("jv-x"), "jevons_agent_start(name=jv-x") {
		t.Fatalf("advice = %q", plan.Advice("jv-x"))
	}
}

func t561Registry(t *testing.T, name string) *claudia.Registry {
	t.Helper()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{Name: name, WorkDir: dir, SessionID: "s-1", Materialized: true, Provider: claudia.ProviderClaude, Purpose: claudia.PurposeWork}); err != nil {
		t.Fatal(err)
	}
	return reg
}
