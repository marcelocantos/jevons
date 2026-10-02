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

// t987Server is the 2026-10-02 incident shape: grok's readings are green,
// claude's weekly is hot (mint-ineligible on its readings), and the owner
// holds grok exhausted through jevons_plan_override. Every mint that day
// was routed by this fixture's logic and landed on grok.
func t987Server(t *testing.T, now time.Time) (*Server, *planusage.OverrideStore) {
	t.Helper()
	s := t583Server(t, func(now time.Time) []planusage.Backend {
		return []planusage.Backend{
			t583Weekly("grok", 60, now),
			t652HotWeekly("claude", now),
		}
	}, now)
	store := planusage.NewOverrideStore(filepath.Join(t.TempDir(), planusage.OverrideFile))
	s.SetPlanOverrides(store)
	inner := s.planUsage
	s.planUsage = func() planusage.Snapshot { return store.Apply(inner()) }
	return s, store
}

func t987SetExhausted(t *testing.T, s *Server, plan, reason string) string {
	t.Helper()
	var req mcp.CallToolRequest
	req.Params.Arguments = map[string]any{"action": "set", "plan": plan, "band": "exhausted", "reason": reason}
	res, err := s.handlePlanOverride(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	text := res.Content[0].(mcp.TextContent).Text
	if res.IsError {
		t.Fatalf("jevons_plan_override band=exhausted refused: %s", text)
	}
	return text
}

// 🎯T987 shape 1: provider omitted. Before the override, the mint resolves
// to grok on its readings; with the owner's exhausted override it must not,
// and with nothing else eligible it is refused, naming the override.
func TestT987BareStartNeverResolvesToOwnerOverriddenExhausted(t *testing.T) {
	now := time.Date(2026, 10, 2, 11, 0, 0, 0, time.UTC)
	s, _ := t987Server(t, now)
	const reason = "Owner: Grok quota is dangerously low; do not seat or migrate work onto it."

	// Control: the readings alone send the mint to grok.
	if pick := s.mintProviderPick("", "", false, "", string(claudia.PurposeWork), "jv-t987-control", false); pick.Provider != cost.HarnessGrok {
		t.Fatalf("control: bare mint on green grok = %+v, want grok", pick)
	}

	text := t987SetExhausted(t, s, "grok", reason)
	if !strings.Contains(text, "no new seat is minted or migrated onto it") {
		t.Fatalf("set result = %q", text)
	}
	if s.providerDestEligible("grok") {
		t.Fatal("fixture: the override must make grok mint-ineligible")
	}
	pick := s.mintProviderPick("", "", false, "", string(claudia.PurposeWork), "jv-t987-bare", false)
	if pick.Provider == cost.HarnessGrok {
		t.Fatalf("bare mint landed on owner-overridden exhausted grok: %+v", pick)
	}
	if pick.Provider != "" || pick.Knob != cost.KnobClaudia {
		t.Fatalf("want a refused claudia pick, got %+v", pick)
	}
	for _, want := range []string{"grok: owner override exhausted (" + reason + ")", "claude: weekly hot"} {
		if !strings.Contains(pick.Detail, want) {
			t.Fatalf("refusal detail %q does not say %q", pick.Detail, want)
		}
	}
}

// 🎯T987 shape 2: provider="claude" was passed and the seat still came up
// grok. Claude's hot weekly drops the pin (🎯T652) and the omit path then
// ranked grok from its readings. The pin may be dropped; the fall-through
// must still honour the override and refuse rather than land on grok.
func TestT987ExplicitClaudeNeverFallsThroughToOverriddenGrok(t *testing.T) {
	now := time.Date(2026, 10, 2, 11, 0, 0, 0, time.UTC)
	s, _ := t987Server(t, now)
	const reason = "Owner: Grok quota is dangerously low."

	// Control: explicit claude on a hot claude fell through to grok.
	if pick := s.mintProviderPick("claude", "", false, "", string(claudia.PurposeWork), "jv-t987-control", false); pick.Provider != cost.HarnessGrok {
		t.Fatalf("control: explicit claude with hot claude = %+v, want the T652 fall-through to grok", pick)
	}

	t987SetExhausted(t, s, "grok", reason)
	pick := s.mintProviderPick("claude", "", false, "", string(claudia.PurposeWork), "jv-t987-explicit", false)
	if pick.Provider == cost.HarnessGrok {
		t.Fatalf("explicit provider=claude resolved to owner-overridden exhausted grok: %+v", pick)
	}
	if pick.Provider != "" {
		t.Fatalf("want a refusal (claude hot, grok kept off), got %+v", pick)
	}
	if !strings.Contains(pick.Detail, "owner override exhausted") {
		t.Fatalf("refusal detail %q does not name the override", pick.Detail)
	}

	// owner_asked keeps the owner's explicit word (🎯T652): claude stays.
	if pick := s.mintProviderPick("claude", "", false, "", string(claudia.PurposeWork), "jv-t987-owner-asked", true); pick.Provider != cost.HarnessClaude || pick.Knob != cost.KnobExplicit {
		t.Fatalf("owner_asked explicit claude = %+v, want explicit claude", pick)
	}
	// An explicit pin on the overridden plan itself is dropped, never honoured.
	if pick := s.mintProviderPick("grok", "", false, "", string(claudia.PurposeWork), "jv-t987-pin-grok", false); pick.Provider == cost.HarnessGrok {
		t.Fatalf("explicit grok pin stuck on an owner-overridden exhausted plan: %+v", pick)
	}
}

// 🎯T987: with another plan eligible, the override simply routes around it.
func TestT987OverrideRoutesAroundTheKeptOffPlan(t *testing.T) {
	now := time.Date(2026, 10, 2, 11, 0, 0, 0, time.UTC)
	s := t583Server(t, func(now time.Time) []planusage.Backend {
		return []planusage.Backend{
			t583Weekly("grok", 60, now),
			t583Weekly("claude", 56, now),
		}
	}, now)
	store := planusage.NewOverrideStore(filepath.Join(t.TempDir(), planusage.OverrideFile))
	s.SetPlanOverrides(store)
	inner := s.planUsage
	s.planUsage = func() planusage.Snapshot { return store.Apply(inner()) }
	t987SetExhausted(t, s, "claude", "Owner: hold Claude for the reset.")
	def, _, note, err := s.stitchAgentStart("jv-t987-route", t.TempDir(), "", "", "", "jevons-po", claudia.PurposeWork, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if def.Provider == claudia.ProviderClaude {
		t.Fatalf("mint landed on owner-overridden exhausted claude: note=%q", note)
	}
	if want := claudia.SubscriptionSeatProvider(claudia.ProviderGrok); def.Provider != want {
		t.Fatalf("want the grok seat provider %q, got %q note=%q", want, def.Provider, note)
	}
}
