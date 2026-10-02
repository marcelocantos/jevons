// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/cli"

	"github.com/marcelocantos/jevons/internal/planusage"
)

// 🎯T691: omit-provider mint with a live plan feed cites claudia as author
// and does not re-adjudicate the pick through PickMintDest / publishedDestEligible.
func TestT691OmitMintCitesClaudia(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	s := t583Server(t, func(now time.Time) []planusage.Backend {
		return []planusage.Backend{
			t583Weekly("grok", 87, now),
			t583Weekly("claude", 56, now),
		}
	}, now)
	def, _, note, err := s.stitchAgentStart(
		"jv-t691-omit", t.TempDir(), "", "", "",
		"jevons-po", claudia.PurposeWork, "", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if cli.PlanProvider(def.Provider) != claudia.ProviderClaude {
		t.Fatalf("omit mint = %q, want a Claude plan seat", def.Provider)
	}
	if !strings.Contains(note, "provider_knob: claudia") {
		t.Fatalf("product pick must name claudia: %q", note)
	}
	if strings.Contains(note, "provider_knob: claude-first") || strings.Contains(note, "provider_knob: plan_dest") {
		t.Fatalf("jevons re-adjudicated the claudia pick: %q", note)
	}
}

// 🎯T691: ahead Claude + hot Grok used to refuse in jevons *before* asking
// claudia (!destOK). The product path now asks claudia; dest-band ranking
// (locked/under/ok) refuses, cited as claudia, not plan_dest/claude-first.
func TestT691AheadOnlyRefusesThroughClaudia(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	s := t583Server(t, func(now time.Time) []planusage.Backend {
		return []planusage.Backend{
			t39015Weekly("claude", 30, 70, now),
			t39015Weekly("grok", 20, 80, now),
		}
	}, now)
	_, _, note, err := s.stitchAgentStart(
		"jv-t691-ahead", t.TempDir(), "", "", "",
		"jevons-po", claudia.PurposeWork, "", "",
	)
	if err == nil || !strings.Contains(err.Error(), "plan dest empty") {
		t.Fatalf("want claudia refuse, err=%v note=%q", err, note)
	}
	if !strings.Contains(note, "provider_knob: claudia") {
		t.Fatalf("refusal must name claudia, not a jevons chooser: %q", note)
	}
	if strings.Contains(note, "provider_knob: claude-first") || strings.Contains(note, "provider_knob: plan_dest") {
		t.Fatalf("jevons re-adjudicated: %q", note)
	}
}
