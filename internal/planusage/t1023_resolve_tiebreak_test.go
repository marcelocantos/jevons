// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/cost"
)

// 🎯T1023: replay the 2026-10-07T03:41:50Z tied reading (scout
// jv-t1023-resolve-quality-scout): claude and codex both ok-band with
// equal weekly slack (29% used); before TieBreak wiring the mint failed
// closed as "token-tied". Portfolio route prefer (claude, codex) plus
// config default (grok) supplies the host TieBreak order.
func TestT1023IncidentResolvesViaPortfolioTieBreak(t *testing.T) {
	th := DefaultThresholds()
	now := time.Date(2026, 10, 7, 3, 41, 50, 0, time.UTC)
	remain := 3*24*time.Hour + 12*time.Hour
	const used = 29.0
	claude := t693weekly("claude", 100-used, used, remain, now)
	codex := t693weekly("codex", 100-used, used, remain, now)

	cands := []DestCand{
		{Provider: "claude", Backend: claude, Load: 2},
		{Provider: "codex", Backend: codex, Load: 1},
	}
	tieBreak := cost.DefaultPortfolio().TieBreakProviders(cost.TaskCodeImplement)

	pick, err := ResolveMint(context.Background(), cands, now, th, tieBreak)
	if err != nil {
		t.Fatalf("ResolveMint refused tied ok-band dests: %v", err)
	}
	if pick.Provider != claudia.ProviderClaude {
		t.Fatalf("tie-break order (route prefer claude before codex): got %+v", pick)
	}
	if !strings.Contains(pick.Reason, "tie_break") {
		t.Fatalf("want tie_break in reason, got %q", pick.Reason)
	}

	dest, ok := PickPlanDest(cands, now, th, tieBreak)
	if !ok || dest != "claude" {
		t.Fatalf("PickPlanDest / ResolveDest path: dest=%q ok=%v", dest, ok)
	}
}
