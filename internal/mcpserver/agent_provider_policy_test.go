// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/planusage"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestT691OwnerProviderPolicyReachesClaudiaPlacementAfterReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	reg, err := claudia.NewRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "worker", SessionID: "session-worker", Provider: claudia.ProviderClaude,
		Purpose: claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	call := func(args map[string]any) *mcp.CallToolResult {
		t.Helper()
		res, err := s.handleAgentProviderPolicy(context.Background(), mcp.CallToolRequest{
			Params: mcp.CallToolParams{Arguments: args},
		})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := call(map[string]any{"name": "worker", "actor": "other", "exclude_providers": []any{"codex"}}); !res.IsError {
		t.Fatal("non-overseer changed provider policy")
	}
	if res := call(map[string]any{"name": "worker", "actor": s.overseerName(),
		"prefer_provider": "openai-codex", "exclude_providers": []any{"codex"}}); res.IsError {
		t.Fatalf("set policy: %s", toolText(res))
	}
	if res := call(map[string]any{"name": "worker", "actor": s.overseerName(),
		"allowed_providers": []any{}}); res.IsError || !strings.Contains(toolText(res), "allowed=none") {
		t.Fatalf("explicit allow-none: %s", toolText(res))
	}
	reloaded, err := claudia.NewRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	s.SetRegistry(reloaded)
	if res := call(map[string]any{"name": "worker"}); res.IsError ||
		!strings.Contains(toolText(res), "allowed=none") || !strings.Contains(toolText(res), "prefer=codex") {
		t.Fatalf("reloaded policy: %s", toolText(res))
	}
	now := time.Now()
	s.SetPlanUsageSource(func() planusage.Snapshot {
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{
			t39015Weekly("claude", 20, 80, now),
			t39015Weekly("codex", 80, 20, now),
			t39015Weekly("grok", 55, 45, now),
		}}
	})
	if got := s.PlanPolicyDecisions(); len(got) != 1 || got[0].Action != claudia.SeatPark || got[0].Author != claudia.DecisionAuthor {
		t.Fatalf("allow-none should park through Claudia: %+v", got)
	}
	if res := call(map[string]any{"name": "worker", "actor": s.overseerName(), "allow_any": true}); res.IsError {
		t.Fatalf("allow-any: %s", toolText(res))
	}
	if got := s.PlanPolicyDecisions(); len(got) != 1 || got[0].To != "grok" {
		t.Fatalf("explicit Codex ban should pick Grok: %+v", got)
	}
	if res := call(map[string]any{"name": "worker", "actor": s.overseerName(), "exclude_providers": []any{}}); res.IsError {
		t.Fatalf("clear exclusions: %s", toolText(res))
	}
	if got := s.PlanPolicyDecisions(); len(got) != 1 || got[0].To != "codex" {
		t.Fatalf("preferred Codex should be eligible again: %+v", got)
	}
}
