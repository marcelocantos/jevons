// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package turnev

import "testing"

// 🎯T417 clause 3: mid-turn absence must not read as lost.
func TestT417MidTurnAbsenceIsUndecidedNotLost(t *testing.T) {
	if got := ReadingFor(FateUnseen, true); got != ReadingUndecided {
		t.Fatalf("composing+unseen reading=%s want undecided (false-negative ban)", got)
	}
	if ReadingFor(FateUnseen, true).PermitsResend() {
		t.Fatal("mid-turn undecided must not permit a re-send")
	}
	// Idle absence still reads as lost — that is the honest idle answer.
	if got := ReadingFor(FateUnseen, false); got != ReadingLost {
		t.Fatalf("idle+unseen reading=%s want lost", got)
	}
	// Positive fates are unchanged whether or not the receiver is composing.
	for _, f := range []Fate{FateUserMessage, FateEnteredTurn, FateQueued} {
		if ReadingFor(f, true) != f.Reading() {
			t.Fatalf("composing mutated positive fate %s", f)
		}
	}
}
