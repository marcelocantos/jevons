// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/planusage"
)

// 🎯T948: the tool sets, reports and clears an override; the override rides
// the plan snapshot, and an omit-provider mint lands on the held plan.
func TestT948PlanOverrideToolAndMint(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	store := planusage.NewOverrideStore(filepath.Join(t.TempDir(), planusage.OverrideFile))
	s.SetPlanOverrides(store)
	s.planUsage = func() planusage.Snapshot {
		return store.Apply(planusage.Snapshot{Backends: []planusage.Backend{
			{Provider: "claude", Status: planusage.StatusAvailable},
			{Provider: "codex", Status: planusage.StatusAvailable},
		}})
	}
	call := func(args map[string]any) (string, bool) {
		t.Helper()
		var req mcp.CallToolRequest
		req.Params.Arguments = args
		res, err := s.handlePlanOverride(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		return res.Content[0].(mcp.TextContent).Text, res.IsError
	}
	if _, isErr := call(map[string]any{"action": "set", "plan": "claude"}); !isErr {
		t.Fatal("an override without a reason was accepted")
	}
	if _, isErr := call(map[string]any{"action": "set", "plan": "claude", "reason": "r", "band": "hot"}); !isErr {
		t.Fatal("an override into a band seats leave was accepted")
	}
	if got := s.planOverrideMint(); got != "" {
		t.Fatalf("mint pinned to %q with no override", got)
	}
	const reason = "Owner has a Claude reset available."
	if text, isErr := call(map[string]any{"action": "set", "plan": "claude", "reason": reason}); isErr {
		t.Fatal(text)
	}
	if text, _ := call(map[string]any{"action": "status"}); !strings.Contains(text, "claude: ok") || !strings.Contains(text, reason) {
		t.Fatalf("status = %q", text)
	}
	if got := s.planOverrideMint(); got != "anthropic" {
		t.Fatalf("mint provider = %q, want anthropic", got)
	}
	if text, isErr := call(map[string]any{"action": "clear", "plan": "claude"}); isErr {
		t.Fatal(text)
	}
	if got := s.planOverrideMint(); got != "" {
		t.Fatalf("mint still pinned to %q after clear", got)
	}
}
