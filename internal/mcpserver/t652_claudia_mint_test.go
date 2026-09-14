// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/planusage"
)

// t652HotWeekly is the T650 incident shape: 85% used at mid-week is BandHot.
func t652HotWeekly(provider string, now time.Time) planusage.Backend {
	used, rem := 85.0, 15.0
	reset := now.Add(time.Duration(float64(planusage.DefaultWeeklyWindowSeconds)*0.5) * time.Second)
	lim := planusage.DefaultWeeklyWindowSeconds
	return planusage.Backend{
		Provider: provider, Status: planusage.StatusAvailable, FetchedAt: now,
		Windows: []planusage.Window{{
			Name: planusage.WindowWeekly, RemainingPercent: &rem, UsedPercent: &used,
			ResetsAt: &reset, LimitWindowSeconds: &lim,
		}},
	}
}

// 🎯T652: PO pins provider=grok while Grok is weekly-hot. That pin is not
// a decision — Claudia / claude-first must land elsewhere.
func TestT652HotExplicitGrokDoesNotStick(t *testing.T) {
	now := time.Date(2026, 9, 14, 11, 0, 0, 0, time.UTC)
	s := t583Server(t, func(now time.Time) []planusage.Backend {
		return []planusage.Backend{
			t652HotWeekly("grok", now),
			t583Weekly("claude", 60, now),
		}
	}, now)
	if s.providerDestEligible("grok") {
		t.Fatal("fixture: grok must be mint-ineligible (weekly hot)")
	}
	if !s.providerDestEligible("claude") {
		t.Fatal("fixture: claude must be mint-eligible")
	}
	def, _, note, err := s.stitchAgentStart(
		"jv-t652-hot-grok", t.TempDir(), "", "grok", "",
		"jevons-po", claudia.PurposeWork, "", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if def.Provider == claudia.ProviderGrok {
		t.Fatalf("hot explicit grok stuck: provider=%q note=%q", def.Provider, note)
	}
	if def.Provider != claudia.ProviderClaude {
		t.Fatalf("want claude (Resolve / claude-first), got %q note=%q", def.Provider, note)
	}
	if strings.Contains(note, "provider_knob: explicit") {
		t.Fatalf("ineligible pin still cited as explicit: %q", note)
	}
}

// 🎯T652: owner_asked is the escape — the owner named that dest.
func TestT652OwnerAskedKeepsHotGrok(t *testing.T) {
	now := time.Date(2026, 9, 14, 11, 0, 0, 0, time.UTC)
	s := t583Server(t, func(now time.Time) []planusage.Backend {
		return []planusage.Backend{
			t652HotWeekly("grok", now),
			t583Weekly("claude", 60, now),
		}
	}, now)
	s.pendingOwnerAsked = true
	def, _, note, err := s.stitchAgentStart(
		"jv-t652-owner-asked", t.TempDir(), "", "grok", "",
		"jevons-po", claudia.PurposeWork, "", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if def.Provider != claudia.ProviderGrok {
		t.Fatalf("owner_asked lost explicit grok: %q note=%q", def.Provider, note)
	}
	if !strings.Contains(note, "provider_knob: explicit") {
		t.Fatalf("owner_asked note = %q", note)
	}
}
