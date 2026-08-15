// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package ctxcap

import "testing"

func TestActionForNeverRemints(t *testing.T) {
	over := Decision{Agent: "jevons", Verdict: VerdictCompact, Context: 105_336, Ceiling: 100_000}
	if got := ActionFor(over); got != ActionObserve {
		t.Fatalf("compact verdict action=%s want observe (remint is withdrawn)", got)
	}
	hold := Decision{Agent: "jevons", Verdict: VerdictHold, Context: 105_336, Ceiling: 100_000}
	if got := ActionFor(hold); got != ActionObserve {
		t.Fatalf("hold verdict action=%s want observe", got)
	}
	ok := Decision{Agent: "jevons", Verdict: VerdictOK, Context: 10, Ceiling: 100_000}
	if got := ActionFor(ok); got != ActionNone {
		t.Fatalf("ok verdict action=%s want none", got)
	}
}
