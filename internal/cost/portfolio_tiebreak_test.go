// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"testing"

	"github.com/marcelocantos/claudia"
)

func TestPortfolioTieBreakHarnessOrderDedupesRouteAndDefault(t *testing.T) {
	p := DefaultPortfolio()
	got := p.TieBreakHarnessOrder(TaskCodeImplement)
	want := []string{HarnessClaude, HarnessCodex, HarnessGrok}
	if len(got) != len(want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order[%d] = %q, want %q (full %v)", i, got[i], want[i], got)
		}
	}
	provs := p.TieBreakProviders(TaskCodeImplement)
	if len(provs) < 2 || provs[0] != claudia.ProviderClaude || provs[1] != claudia.ProviderCodex {
		t.Fatalf("providers = %v, want claude then codex …", provs)
	}
}
