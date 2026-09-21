// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func toolsNamed(names ...string) []mcp.Tool {
	out := make([]mcp.Tool, len(names))
	for i, n := range names {
		out[i] = mcp.NewTool(n)
	}
	return out
}

func listedFor(t *testing.T, url string, tools []mcp.Tool) map[string]bool {
	t.Helper()
	ctx := toolProfileFromRequest(context.Background(), httptest.NewRequest("POST", url, nil))
	got := map[string]bool{}
	for _, tool := range filterToolsByProfile(ctx, tools) {
		got[tool.Name] = true
	}
	return got
}

// The full surface was 55 tools on 2026-09-22; the core profile is what a
// seat that asks for it is listed instead.
func TestCoreProfileListsFleetControlAndNotAmbientKnobs(t *testing.T) {
	all := toolsNamed(
		"jevons_agent_list", "jevons_agent_start", "jevons_agent_send", "jevons_target_file",
		"jevons_gate_show", "jevons_sendq_reconcile", "jwork",
		"jevons_audit_configure", "jevons_research_cycle", "jevons_rsi_coach_status",
		"jevons_idea_list", "jevons_thread_spawn", "self_test.run", "jevons_writ_exec",
	)

	core := listedFor(t, "/mcp?tools=core", all)
	for _, want := range []string{"jevons_agent_list", "jevons_agent_start", "jevons_agent_send",
		"jevons_target_file", "jevons_gate_show", "jevons_sendq_reconcile", "jwork"} {
		if !core[want] {
			t.Errorf("core profile does not list %s", want)
		}
	}
	for _, not := range []string{"jevons_audit_configure", "jevons_research_cycle",
		"jevons_rsi_coach_status", "jevons_idea_list", "jevons_thread_spawn", "self_test.run", "jevons_writ_exec"} {
		if core[not] {
			t.Errorf("core profile lists %s", not)
		}
	}

	// The controls: no profile, and a profile this daemon has not heard of,
	// both list everything. Asking for an unknown surface must not yield none.
	for _, url := range []string{"/mcp", "/mcp?tools=overseer-v9"} {
		if got := listedFor(t, url, all); len(got) != len(all) {
			t.Errorf("%s listed %d of %d tools", url, len(got), len(all))
		}
	}
}

// liveSurface is what GET tools/list returned from the development daemon on
// 2026-09-22: 55 tools, 57,943 bytes of schema.
func liveSurface() []mcp.Tool {
	return toolsNamed(
		"jevons_active_work", "jevons_agent_kill", "jevons_agent_list", "jevons_agent_migrate",
		"jevons_agent_report_read", "jevons_agent_send", "jevons_agent_start", "jevons_agent_stop",
		"jevons_audit_configure", "jevons_audit_cycle", "jevons_audit_report",
		"jevons_audit_residue", "jevons_audit_status", "jevons_capacity_status", "jevons_cost",
		"jevons_event_push", "jevons_fleet_intent", "jevons_gate_show", "jevons_idea_capture",
		"jevons_idea_list", "jevons_idea_triage", "jevons_job", "jevons_logs_tail",
		"jevons_mcp_reconnect", "jevons_owner_gate", "jevons_plan_usage",
		"jevons_research_configure", "jevons_research_cycle", "jevons_research_list",
		"jevons_research_read", "jevons_research_status", "jevons_rsi_coach_configure",
		"jevons_rsi_coach_cycle", "jevons_rsi_coach_status", "jevons_rsi_disposition",
		"jevons_screenshot", "jevons_security_status", "jevons_sendq_reconcile",
		"jevons_sentinel_cycle", "jevons_spawn_order", "jevons_staff_ops_cycle",
		"jevons_target_file", "jevons_thread_adopt", "jevons_thread_direct", "jevons_thread_list",
		"jevons_thread_remove", "jevons_thread_spawn", "jevons_thread_status",
		"jevons_thread_takeover", "jevons_transcript_read", "jevons_transcript_rewind",
		"jevons_writ_exec", "jwork", "self_test.list", "self_test.run",
	)
}

// Under the core profile that surface stays under forty tools and still
// carries what drives a fleet.
func TestCoreProfileOfTheLiveSurfaceFitsFortyTools(t *testing.T) {
	all := liveSurface()
	core := listedFor(t, "/mcp?tools=core", all)
	if len(all) != 55 || len(core) > 40 {
		t.Fatalf("core profile lists %d of %d tools; want at most 40 of 55", len(core), len(all))
	}
	for _, want := range []string{"jevons_agent_list", "jevons_agent_start", "jevons_agent_send",
		"jevons_agent_stop", "jevons_agent_report_read", "jevons_agent_migrate", "jevons_target_file",
		"jevons_gate_show", "jevons_sendq_reconcile", "jevons_spawn_order", "jevons_fleet_intent",
		"jevons_capacity_status", "jevons_plan_usage", "jevons_owner_gate"} {
		if !core[want] {
			t.Errorf("core profile does not list %s", want)
		}
	}
}
