// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"context"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T652: ResolveMint skips a weekly-hot dest and prefers Claude.
func TestResolveMintSkipsHotGrok(t *testing.T) {
	now := time.Date(2026, 9, 14, 11, 0, 0, 0, time.UTC)
	th := DefaultThresholds()
	cands := []DestCand{
		{Provider: "grok", Backend: t495Backend("grok", t495pf(85), t495pf(15), t495Week(0.5), now)},
		{Provider: "claude", Backend: t495Backend("claude", t495pf(40), t495pf(60), t495Week(0.5), now)},
	}
	if DestEligible(cands[0].Backend, now, th) {
		t.Fatal("fixture: grok must be ineligible")
	}
	pick, err := ResolveMint(context.Background(), cands, now, th)
	if err != nil {
		t.Fatal(err)
	}
	if pick.Provider != claudia.ProviderClaude {
		t.Fatalf("ResolveMint = %q, want claude (hot grok skipped)", pick.Provider)
	}
}
