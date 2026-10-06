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
// a decision — Claudia must land elsewhere.
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
		t.Fatalf("want claude (the only published, eligible dest left), got %q note=%q", def.Provider, note)
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

// t652SessionLowWeeklyOK is the other half of mint-ineligible: the weekly
// band is a healthy ok, but the session window is at the low threshold, so
// the dest empties mid-turn. T652 clause 2 names session-low alongside
// weekly hot/exhausted; only the weekly half was taped.
func t652SessionLowWeeklyOK(provider string, now time.Time, th planusage.Thresholds) planusage.Backend {
	used, rem := 40.0, 60.0
	reset := now.Add(time.Duration(float64(planusage.DefaultWeeklyWindowSeconds)*0.5) * time.Second)
	lim := planusage.DefaultWeeklyWindowSeconds
	low := th.LowRemainingPercent
	sused := 100 - low
	sreset := now.Add(2 * time.Hour)
	return planusage.Backend{
		Provider: provider, Status: planusage.StatusAvailable, FetchedAt: now,
		Windows: []planusage.Window{
			{
				Name: planusage.WindowWeekly, RemainingPercent: &rem, UsedPercent: &used,
				ResetsAt: &reset, LimitWindowSeconds: &lim,
			},
			{
				Name: planusage.WindowSession, RemainingPercent: &low, UsedPercent: &sused,
				ResetsAt: &sreset,
			},
		},
	}
}

// 🎯T652 clause 2, session-low half: a PO pins provider=grok while Grok's
// weekly is fine but its session window is low. A dest that empties
// mid-turn is not a decision either — the pin is dropped and Claudia /
// Claudia lands elsewhere. Guards the seam where mintProviderPick
// drops the pin on providerDestEligible (the T693 dest-band bar) rather
// than planusage.MintIneligible: the two agree on session-low today, and
// this tape is what fails if that stops being true.
func TestT652SessionLowExplicitGrokDoesNotStick(t *testing.T) {
	now := time.Date(2026, 9, 14, 11, 0, 0, 0, time.UTC)
	th := planusage.DefaultThresholds()
	s := t583Server(t, func(now time.Time) []planusage.Backend {
		return []planusage.Backend{
			t652SessionLowWeeklyOK("grok", now, th),
			t583Weekly("claude", 60, now),
		}
	}, now)
	if planusage.WeeklyBandOf(t652SessionLowWeeklyOK("grok", now, th), now, th) == planusage.BandHot {
		t.Fatal("fixture: grok weekly must NOT be hot — this tape is the session-low half")
	}
	if s.providerDestEligible("grok") {
		t.Fatal("fixture: grok must be mint-ineligible (session low)")
	}
	def, _, note, err := s.stitchAgentStart(
		"jv-t652-session-low", t.TempDir(), "", "grok", "",
		"jevons-po", claudia.PurposeWork, "", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if def.Provider != claudia.ProviderClaude {
		t.Fatalf("session-low explicit grok stuck: provider=%q note=%q", def.Provider, note)
	}
	if strings.Contains(note, "provider_knob: explicit") {
		t.Fatalf("ineligible pin still cited as explicit: %q", note)
	}
}

// 🎯T652 clause 1 names thread_spawn alongside agent_start. butler.Spawn
// launches a real process, so this tape drives the decision seam
// handleThreadSpawn uses (threads.go: mintProviderPick with the thread's
// id as name, purpose=work, no task_type) rather than the handler itself.
// It pins the decision, not the handler's wiring to it — a caller that
// stopped consulting mintProviderPick would still pass.
func TestT652ThreadSpawnShapeDropsHotPin(t *testing.T) {
	now := time.Date(2026, 9, 14, 11, 0, 0, 0, time.UTC)
	s := t583Server(t, func(now time.Time) []planusage.Backend {
		return []planusage.Backend{
			t652HotWeekly("grok", now),
			t583Weekly("claude", 60, now),
		}
	}, now)
	// The thread_spawn call shape: explicit grok, fresh mint, not owner_asked.
	pick := s.mintProviderPick("grok", "", false, "", string(claudia.PurposeWork), "jv-t652-thread", false)
	if pick.Provider != string(claudia.ProviderClaude) {
		t.Fatalf("thread_spawn hot grok pin stuck: %+v", pick)
	}
	if strings.Contains(pick.Cite(), "provider_knob: explicit") {
		t.Fatalf("ineligible thread pin cited as explicit: %q", pick.Cite())
	}
	// owner_asked is the same escape on this path.
	owned := s.mintProviderPick("grok", "", false, "", string(claudia.PurposeWork), "jv-t652-thread", true)
	if owned.Provider != string(claudia.ProviderGrok) {
		t.Fatalf("owner_asked lost explicit grok on thread shape: %+v", owned)
	}
}
