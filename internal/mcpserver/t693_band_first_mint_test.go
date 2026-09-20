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

func t693Weekly(provider string, rem, used float64, remain time.Duration, now time.Time) planusage.Backend {
	pctRem, pctUsed := rem, used
	resets := now.Add(remain)
	lim := planusage.DefaultWeeklyWindowSeconds
	return planusage.Backend{
		Provider: provider, Status: planusage.StatusAvailable, FetchedAt: now,
		Windows: []planusage.Window{{
			Name: planusage.WindowWeekly, RemainingPercent: &pctRem, UsedPercent: &pctUsed,
			ResetsAt: &resets, LimitWindowSeconds: &lim,
		}},
	}
}

// 🎯T693 production mint path: omit-provider stitchAgentStart with config
// grok. Claude weekly under (~71%, ~17h) vs Grok weekly ok (~5%, ~137h)
// must land on Claude — remaining-% / slack would pick Grok.
func TestT693OmitMintUnderBeatsOk(t *testing.T) {
	now := time.Date(2026, 9, 20, 20, 0, 0, 0, time.UTC)
	s := t583Server(t, func(now time.Time) []planusage.Backend {
		return []planusage.Backend{
			t693Weekly("claude", 29, 71, 17*time.Hour, now),
			t693Weekly("grok", 95, 5, 137*time.Hour, now),
		}
	}, now)
	def, _, note, err := s.stitchAgentStart(
		"jv-t693-under", t.TempDir(), "", "", "",
		"jevons-po", claudia.PurposeWork, "", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if def.Provider != claudia.ProviderClaude {
		t.Fatalf("omit mint = %q, want claude (under beats ok despite Grok slack)", def.Provider)
	}
	if !strings.Contains(note, "provider_knob: claudia") {
		t.Fatalf("production pick must name claudia, not plan_dest/PickMintDest: %q", note)
	}
	if strings.Contains(note, "provider_knob: plan_dest") || strings.Contains(note, "provider_knob: claude-first") {
		t.Fatalf("jevons re-adjudicated the claudia pick: %q", note)
	}
}

// 🎯T693: Fable weekly_model at 0% does not veto Claude on the mint path.
func TestT693OmitMintFableSpentDoesNotVetoClaude(t *testing.T) {
	now := time.Date(2026, 9, 20, 20, 0, 0, 0, time.UTC)
	pct := func(v float64) *float64 { return &v }
	resets := now.Add(17 * time.Hour)
	lim := planusage.DefaultWeeklyWindowSeconds
	s := t583Server(t, func(now time.Time) []planusage.Backend {
		return []planusage.Backend{
			{
				Provider: "claude", Status: planusage.StatusAvailable, FetchedAt: now,
				Windows: []planusage.Window{
					{
						Name: planusage.WindowModelWeekly, Model: "Fable",
						RemainingPercent: pct(0), UsedPercent: pct(100),
						ResetsAt: &resets, LimitWindowSeconds: &lim,
					},
					{
						Name: planusage.WindowWeekly, RemainingPercent: pct(29), UsedPercent: pct(71),
						ResetsAt: &resets, LimitWindowSeconds: &lim,
					},
					{
						Name: planusage.WindowSession, RemainingPercent: pct(80), UsedPercent: pct(20),
					},
				},
			},
			t693Weekly("grok", 95, 5, 137*time.Hour, now),
		}
	}, now)
	def, _, note, err := s.stitchAgentStart(
		"jv-t693-fable", t.TempDir(), "", "", "",
		"jevons-po", claudia.PurposeWork, "", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if def.Provider != claudia.ProviderClaude {
		t.Fatalf("Fable spent ≠ Claude unavailable: mint=%q note=%q", def.Provider, note)
	}
	if def.Model == "claude-fable-5" {
		t.Fatalf("landed on spent Fable: %+v", def)
	}
	if !strings.Contains(note, "provider_knob: claudia") {
		t.Fatalf("production pick must name claudia: %q", note)
	}
}
