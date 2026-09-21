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

// 🎯T791: claude at its soft cap + cursor with plan headroom (and no cursor
// cap) must refuse the mint — cursor seats cannot be steered (claudia T118),
// so headroom does not make it a destination.
func t791Server(t *testing.T, claudeLoad int) *Server {
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
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{
			t583Weekly("claude", 56, now),
			t583Weekly("cursor", 90, now),
		}}
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

// The exclusion is a capability flag: lifting it re-admits the provider.
func TestT791ExclusionLiftsWithCapability(t *testing.T) {
	restore := planusage.SetUnsteerableForTest(map[string]string{})
	defer restore()
	s := t791Server(t, 12)
	pick := s.mintProviderPick("", "", false, "code_implement", string(claudia.PurposeWork), "jv-t791-lift", false)
	if pick.Provider != "cursor" {
		t.Fatalf("steerable cursor not a dest after lift: %+v", pick)
	}
}

// t791Steerable simulates the claudia T118/T119 capability having landed, for
// tests whose subject is something other than the steerability exclusion
// (model pins, cursor start delivery, dest ranking among plan backends).
func t791Steerable(t *testing.T) {
	t.Helper()
	t.Cleanup(planusage.SetUnsteerableForTest(map[string]string{}))
}
