// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package ctxcap

import "testing"

func TestActionForNeverRemints(t *testing.T) {
	over := Decision{Agent: "jevons", Verdict: VerdictCompact, Context: 105_336, Ceiling: 100_000}
	if got := ActionFor(over); got != ActionUnworkable {
		t.Fatalf("compact verdict action=%s want unworkable (remint is withdrawn; 🎯T417)", got)
	}
	hold := Decision{Agent: "jevons", Verdict: VerdictHold, Context: 105_336, Ceiling: 100_000}
	if got := ActionFor(hold); got != ActionUnworkable {
		t.Fatalf("hold verdict action=%s want unworkable", got)
	}
	ok := Decision{Agent: "jevons", Verdict: VerdictOK, Context: 10, Ceiling: 100_000}
	if got := ActionFor(ok); got != ActionNone {
		t.Fatalf("ok verdict action=%s want none", got)
	}
	if Unworkable(ok) {
		t.Fatal("ok decision must not be Unworkable")
	}
	if !Unworkable(hold) {
		t.Fatal("hold decision must be Unworkable")
	}
}
