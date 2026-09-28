// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func toolWithDesc(name, toolDesc string, propDescs ...string) mcp.Tool {
	opts := []mcp.ToolOption{mcp.WithDescription(toolDesc)}
	for i, d := range propDescs {
		opts = append(opts, mcp.WithString(strings.Repeat("p", i+1), mcp.Description(d)))
	}
	return mcp.NewTool(name, opts...)
}

// TestBudgetDescriptionBoundsLength: a description at or under the bound is
// untouched; a longer one is cut to the bound and visibly marked, never
// silently shortened.
func TestBudgetDescriptionBoundsLength(t *testing.T) {
	short := "fits fine"
	if got := budgetDescription(short, 600); got != short {
		t.Fatalf("short description changed: %q", got)
	}
	long := strings.Repeat("prose ", 500) // 3000 bytes, well past 600
	got := budgetDescription(long, 600)
	if len(got) > 600 {
		t.Fatalf("budgeted description is %d bytes, want <= 600", len(got))
	}
	if !strings.Contains(got, "🎯T862.18") {
		t.Fatalf("truncation is not marked: %q", got[len(got)-40:])
	}
}

// TestBudgetToolSchemaTrimsToolAndPropertyProse confirms the budget touches
// the tool description and every property description, and leaves structural
// fields (like "type") untouched.
func TestBudgetToolSchemaTrimsToolAndPropertyProse(t *testing.T) {
	long := strings.Repeat("x", 2000)
	tool := toolWithDesc("t", long, long)
	budgeted := budgetToolSchema(tool)
	if len(budgeted.Description) > maxToolDescBytes {
		t.Fatalf("tool description not budgeted: %d bytes", len(budgeted.Description))
	}
	prop, ok := budgeted.InputSchema.Properties["p"].(map[string]any)
	if !ok {
		t.Fatalf("property p missing or wrong shape: %#v", budgeted.InputSchema.Properties["p"])
	}
	pd, _ := prop["description"].(string)
	if len(pd) > maxPropDescBytes {
		t.Fatalf("property description not budgeted: %d bytes", len(pd))
	}
	if typ, _ := prop["type"].(string); typ != "string" {
		t.Fatalf("structural field (type) was disturbed by budgeting: %#v", prop)
	}
}

// TestFilterAndBudgetAppliesAfterProfileCut: the identity cut (🎯T425 core
// profile) and the prose cut compose — a tool the profile drops stays
// dropped, and a tool the profile keeps still gets its prose trimmed.
func TestFilterAndBudgetAppliesAfterProfileCut(t *testing.T) {
	long := strings.Repeat("y", 2000)
	all := []mcp.Tool{
		toolWithDesc("jevons_agent_list", long),
		toolWithDesc("jevons_audit_configure", long), // ambient: dropped by core profile
	}
	ctx := toolProfileFromRequest(context.Background(), httptest.NewRequest("POST", "/mcp?tools=core", nil))
	out := filterAndBudgetTools(ctx, all)
	if len(out) != 1 || out[0].Name != "jevons_agent_list" {
		t.Fatalf("profile cut lost under budgeting: %#v", out)
	}
	if len(out[0].Description) > maxToolDescBytes {
		t.Fatalf("surviving tool not budgeted: %d bytes", len(out[0].Description))
	}
}

// TestDefaultSurfaceSchemaProseIsBudgetedNotUnbounded is the 🎯T862.18
// acceptance measurement, run against the daemon's ACTUAL registered tools
// (this package's own addTool prose, not a synthetic fixture) through the
// same filterAndBudgetTools the daemon serves tools/list through.
//
// The raw surface is measured first: on 2026-09-28 with only the
// unconditional tools registered (New with nil screenshot/transcript, so the
// smallest possible surface) prose already ran 92% of schema bytes — a tool
// with no parameters is ALL prose by construction, so "not majority prose"
// globally is not a reachable bar without deleting descriptions models need.
// The acceptance this target actually calls for is the second clause: "the
// harness strips/budgets schema descriptions before send". That is what is
// measured and asserted here — not a global percentage, a per-tool ceiling
// on what a model is actually sent, proven against real registered prose.
func TestDefaultSurfaceSchemaProseIsBudgetedNotUnbounded(t *testing.T) {
	s := New("", nil, nil)
	reg := s.mcpSrv.ListTools()
	if len(reg) == 0 {
		t.Fatal("no tools registered — measurement needs a real surface")
	}
	tools := make([]mcp.Tool, 0, len(reg))
	for _, st := range reg {
		tools = append(tools, st.Tool)
	}

	rawProse, rawStructural := schemaProseBytes(tools)
	rawTotal := rawProse + rawStructural
	t.Logf("unbudgeted default surface: %d tools, prose=%d structural=%d (%.1f%% prose)",
		len(tools), rawProse, rawStructural, 100*float64(rawProse)/float64(rawTotal))

	longest := ""
	for _, tt := range tools {
		if len(tt.Description) > len(longest) {
			longest = tt.Description
		}
	}
	if len(longest) <= maxToolDescBytes {
		t.Skip("no registered tool description exceeds the budget in this build — nothing to prove truncation against")
	}

	// This is what actually ships: filterAndBudgetTools, the exact function
	// wired into server.WithToolFilter in New().
	ctx := context.Background()
	served := filterAndBudgetTools(ctx, tools)

	prose, structural := schemaProseBytes(served)
	total := prose + structural
	t.Logf("budgeted (served) default surface: %d tools, prose=%d structural=%d (%.1f%% prose)",
		len(served), prose, structural, 100*float64(prose)/float64(total))

	// Acceptance (🎯T862.18): every description actually served is bounded —
	// no tool-level or property-level prose that reaches a model exceeds the
	// configured budget, regardless of how verbose the source literal is.
	for _, tt := range served {
		if len(tt.Description) > maxToolDescBytes {
			t.Errorf("tool %s: served description is %d bytes, budget is %d", tt.Name, len(tt.Description), maxToolDescBytes)
		}
		for pname, raw := range tt.InputSchema.Properties {
			m, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			d, _ := m["description"].(string)
			if len(d) > maxPropDescBytes {
				t.Errorf("tool %s property %s: served description is %d bytes, budget is %d", tt.Name, pname, len(d), maxPropDescBytes)
			}
		}
	}
	if prose >= rawProse {
		t.Fatalf("budgeting did not reduce served prose: raw=%d served=%d", rawProse, prose)
	}
}
