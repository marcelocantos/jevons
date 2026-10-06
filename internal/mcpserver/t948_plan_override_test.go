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
	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/planusage"
)

// 🎯T948 / 🎯T1013.1: the tool sets, reports and clears an override; the
// override rides the plan snapshot, and an omit-provider mint lands on the
// held plan through the real mintProviderPick → claudia.Resolve path — not
// through a jevons-side pin injected before ever calling Resolve.
func TestT948PlanOverrideToolAndMint(t *testing.T) {
	now := time.Date(2026, 10, 6, 11, 0, 0, 0, time.UTC)
	s := t583Server(t, func(now time.Time) []planusage.Backend {
		return []planusage.Backend{
			t652HotWeekly("claude", now),
			t583Weekly("codex", 60, now),
		}
	}, now)
	store := planusage.NewOverrideStore(filepath.Join(t.TempDir(), planusage.OverrideFile))
	s.SetPlanOverrides(store)
	inner := s.planUsage
	s.planUsage = func() planusage.Snapshot { return store.Apply(inner()) }

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

	// Without an override, claude's own hot weekly means the omit-provider
	// mint cannot land there — codex (the only readable dest-band plan)
	// wins on its readings.
	if pick := s.mintProviderPick("", "", false, "", string(claudia.PurposeWork), "jv-t948-control", false); pick.Provider != cost.HarnessCodex {
		t.Fatalf("control: bare mint with claude hot = %+v, want codex", pick)
	}

	const reason = "Owner has a Claude reset available."
	if text, isErr := call(map[string]any{"action": "set", "plan": "claude", "reason": reason}); isErr {
		t.Fatal(text)
	}
	if text, _ := call(map[string]any{"action": "status"}); !strings.Contains(text, "claude: ok") || !strings.Contains(text, reason) {
		t.Fatalf("status = %q", text)
	}

	// 🎯T1013.1: the override is now an input Resolve itself applies — it
	// pins claude as the mint pick even though codex's own readings are
	// perfectly healthy and would otherwise win. No jevons-side pin
	// injection runs before this call.
	pick := s.mintProviderPick("", "", false, "", string(claudia.PurposeWork), "jv-t948-overridden", false)
	if pick.Provider != cost.HarnessClaude {
		t.Fatalf("mint provider = %+v, want claude (owner override pin)", pick)
	}

	if text, isErr := call(map[string]any{"action": "clear", "plan": "claude"}); isErr {
		t.Fatal(text)
	}
	if pick := s.mintProviderPick("", "", false, "", string(claudia.PurposeWork), "jv-t948-cleared", false); pick.Provider != cost.HarnessCodex {
		t.Fatalf("mint still pinned to claude after clear: %+v, want codex", pick)
	}
}
