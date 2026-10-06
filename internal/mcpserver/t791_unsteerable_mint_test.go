// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/planusage"
)

// 🎯T791: claude at its soft cap + a provider in the unsteerable table with
// plan headroom must refuse the mint — headroom does not make an unsteerable
// provider a destination. The table is empty since 🎯T841, so these mechanism
// tests name cursor in a synthetic table.
func t791Server(t *testing.T, claudeLoad int, extra ...string) *Server {
	t.Helper()
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.SetDefaultProvider(string(claudia.ProviderClaude))
	s.SetProviderSoftCaps(map[string]int{"claude": 12})
	s.SetPlanUsageSource(func() planusage.Snapshot {
		bes := []planusage.Backend{
			t583Weekly("claude", 56, now),
			t583Weekly("cursor", 90, now),
		}
		for _, p := range extra {
			bes = append(bes, t583Weekly(p, 90, now))
		}
		return planusage.Snapshot{At: now, Backends: bes}
	})
	for i := 0; i < claudeLoad; i++ {
		if err := reg.Register(claudia.AgentDef{
			Name: fmt.Sprintf("fill-claude-%d", i), WorkDir: t.TempDir(),
			SessionID: fmt.Sprintf("claude-%d", i), Provider: claudia.ProviderClaude,
		}); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func TestT791ClaudeAtCapCursorHeadroomRefusesMint(t *testing.T) {
	defer planusage.SetUnsteerableForTest(map[string]string{"cursor": "synthetic exclusion"})()
	s := t791Server(t, 12)
	pick := s.mintProviderPick("", "", false, "code_implement", string(claudia.PurposeWork), "jv-t791-omit", false)
	if strings.TrimSpace(pick.Provider) != "" {
		t.Fatalf("mint landed on unsteerable %q: %+v", pick.Provider, pick)
	}
	for _, want := range []string{"soft cap", "claude", "cursor", "unsteerable"} {
		if !strings.Contains(pick.Detail, want) {
			t.Fatalf("refusal detail must name %q: %q", want, pick.Detail)
		}
	}
	if _, _, _, err := s.stitchAgentStart("jv-t791-stitch", t.TempDir(), "", "", "code_implement",
		"jevons-po", claudia.PurposeWork, "", ""); err == nil {
		t.Fatal("stitch minted a seat when only an unsteerable dest has room")
	}

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "jv-t791-start", "workdir": t.TempDir(), "purpose": "work"}
	res, err := s.handleAgentStart(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || !res.IsError {
		t.Fatal("handleAgentStart must refuse")
	}
	got := toolText(res)
	for _, want := range []string{"soft cap", "claude", "cursor"} {
		if !strings.Contains(got, want) {
			t.Fatalf("start refusal must name %q: %s", want, got)
		}
	}
}

// An explicit cursor pin is a habit, not a decision, unless owner_asked.
func TestT791ExplicitCursorPinDroppedUnlessOwnerAsked(t *testing.T) {
	defer planusage.SetUnsteerableForTest(map[string]string{"cursor": "synthetic exclusion"})()
	s := t791Server(t, 0)
	pick := s.mintProviderPick("cursor", "", false, "code_implement", string(claudia.PurposeWork), "jv-t791-pin", false)
	if pick.Provider != "claude" {
		t.Fatalf("explicit cursor stuck: %+v", pick)
	}
	if !strings.Contains(pick.Detail, "cursor") {
		t.Fatalf("start result must cite the exclusion: %q", pick.Detail)
	}
	pick = s.mintProviderPick("cursor", "", false, "code_implement", string(claudia.PurposeWork), "jv-t791-pin2", true)
	if pick.Provider != "cursor" {
		t.Fatalf("owner_asked cursor dropped: %+v", pick)
	}
}

// 🎯T841: codex is a destination again (the claudia bug that rejected its
// seats' MCP calls is fixed, 5fa7f35/v0.42.0). 🎯T1013.3: cursor, by
// contrast, is genuinely still excluded — claudia.Steerable reports
// claudia T118's open tool-budget defect — so this exercises the real
// table, not a synthetic one via SetUnsteerableForTest.
func TestT791CodexIsDestinationCursorStaysExcluded(t *testing.T) {
	if why := planusage.UnsteerableReason("codex"); why != "" {
		t.Fatalf("codex still unsteerable: %q", why)
	}
	if why := planusage.UnsteerableReason("cursor"); why == "" {
		t.Fatal("cursor must report unsteerable: claudia T118 is open")
	}

	// Claude at cap, only cursor with headroom: refused, not landed on cursor.
	s := t791Server(t, 12)
	pick := s.mintProviderPick("", "", false, "code_implement", string(claudia.PurposeWork), "jv-t841-cursor", false)
	if strings.TrimSpace(pick.Provider) != "" {
		t.Fatalf("omitted-provider mint landed on unsteerable cursor: %+v", pick)
	}

	// Claude at cap, only codex with headroom: lands on codex, not refused.
	s = t791Server(t, 12, "codex")
	s.SetPlanUsageSource(func() planusage.Snapshot {
		now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{
			t583Weekly("claude", 56, now), t583Weekly("codex", 90, now),
		}}
	})
	pick = s.mintProviderPick("", "", false, "code_implement", string(claudia.PurposeWork), "jv-t841-omit", false)
	if pick.Provider != "codex" {
		t.Fatalf("omitted-provider mint did not land on codex: %+v", pick)
	}
	if strings.Contains(pick.Detail, "excluded unsteerable") {
		t.Fatalf("exclusion cited: %q", pick.Detail)
	}

	// An explicit codex pin is honoured without owner_asked.
	s = t791Server(t, 0, "codex")
	pick = s.mintProviderPick("codex", "", false, "code_implement", string(claudia.PurposeWork), "jv-t841-pin-codex", false)
	if pick.Provider != "codex" {
		t.Fatalf("explicit codex dropped: %+v", pick)
	}

	// An explicit cursor pin is dropped unless owner_asked (same shape as
	// TestT791ExplicitCursorPinDroppedUnlessOwnerAsked, against the real
	// table rather than a synthetic one).
	s = t791Server(t, 0, "codex")
	pick = s.mintProviderPick("cursor", "", false, "code_implement", string(claudia.PurposeWork), "jv-t841-pin-cursor", false)
	if pick.Provider == "cursor" {
		t.Fatalf("explicit cursor pin was honoured without owner_asked: %+v", pick)
	}
}

// t791Steerable simulates an empty exclusion table (the state since 🎯T841), for
// tests whose subject is something other than the steerability exclusion
// (model pins, cursor start delivery, dest ranking among plan backends).
func t791Steerable(t *testing.T) {
	t.Helper()
	t.Cleanup(planusage.SetUnsteerableForTest(map[string]string{}))
}
