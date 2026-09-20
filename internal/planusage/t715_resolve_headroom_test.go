// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"context"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T715: omit-provider ResolveMint skips a dest at its published session
// cap and lands on a dest with headroom. Plan-band eligibility is not
// enough — claude 12/12 with weekly headroom is not a destination.
func TestT715ResolveMintSkipsExhaustedPin(t *testing.T) {
	now := time.Date(2026, 9, 21, 0, 50, 0, 0, time.UTC)
	th := DefaultThresholds()
	claude := t495Backend("claude", t495pf(40), t495pf(60), t495Week(0.5), now)
	grok := t495Backend("grok", t495pf(20), t495pf(80), t495Week(0.5), now)
	if !DestEligible(claude, now, th) || !DestEligible(grok, now, th) {
		t.Fatal("fixture: both dests must be plan-eligible — this tape is session-cap, not weekly-hot")
	}
	pick, err := ResolveMint(context.Background(), []DestCand{
		{Provider: "claude", Backend: claude, Load: 12, Cap: 12},
		{Provider: "grok", Backend: grok, Load: 0, Cap: 12},
	}, now, th)
	if err != nil {
		t.Fatal(err)
	}
	if pick.Provider != claudia.ProviderGrok {
		t.Fatalf("ResolveMint = %q, want grok (claude at session cap skipped)", pick.Provider)
	}
}

// 🎯T715 mutation: restoring pin-first resolution when the pin is exhausted
// goes RED. Cap omitted (zero) is unpublished — destAtSessionCap does not
// skip — so prefer=claude still wins despite Load 12. The skip-at-cap tape
// above fails if destAtSessionCap is deleted; this control pins that Cap
// is the lever, not Load alone.
func TestT715MutationPinFirstWhenPinExhaustedGoesRed(t *testing.T) {
	now := time.Date(2026, 9, 21, 0, 50, 0, 0, time.UTC)
	th := DefaultThresholds()
	pick, err := ResolveMint(context.Background(), []DestCand{
		{Provider: "claude", Backend: t495Backend("claude", t495pf(40), t495pf(60), t495Week(0.5), now), Load: 12},
		{Provider: "grok", Backend: t495Backend("grok", t495pf(20), t495pf(80), t495Week(0.5), now), Load: 0, Cap: 12},
	}, now, th)
	if err != nil {
		t.Fatal(err)
	}
	if pick.Provider != claudia.ProviderClaude {
		t.Fatalf("without Cap on the pin, ResolveMint = %q want claude (pin-first); Cap is the skip lever", pick.Provider)
	}
}

func TestT715ResolveMintAllAtCapRefuses(t *testing.T) {
	now := time.Date(2026, 9, 21, 0, 50, 0, 0, time.UTC)
	th := DefaultThresholds()
	_, err := ResolveMint(context.Background(), []DestCand{
		{Provider: "claude", Backend: t495Backend("claude", t495pf(40), t495pf(60), t495Week(0.5), now), Load: 12, Cap: 12},
		{Provider: "grok", Backend: t495Backend("grok", t495pf(20), t495pf(80), t495Week(0.5), now), Load: 12, Cap: 12},
		{Provider: "codex", Backend: t495Backend("codex", t495pf(30), t495pf(70), t495Week(0.5), now), Load: 6, Cap: 6},
	}, now, th)
	if err == nil {
		t.Fatal("every dest at cap must refuse")
	}
}
