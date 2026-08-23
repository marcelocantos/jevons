// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr

import "testing"

func TestT454ClassifyTurnOutput(t *testing.T) {
	spend := "You've hit your monthly spend limit. Run /usage-credits to manage your limit and keep using Fable 5 or switch models to continue this chat."
	auth := "401 Unauthorized: invalid api key"
	revoked := "Your API key has been revoked"
	mixed := "Hit the spend limit earlier; I've started on T454 and wrote the classifier test."
	work := "Done — implemented refusal-hold and gated the suite."

	cases := []struct {
		name string
		text string
		want TurnKind
	}{
		{"empty", "", TurnEmpty},
		{"spend_limit", spend, TurnRefusalOnly},
		{"auth", auth, TurnRefusalOnly},
		{"revoked", revoked, TurnRefusalOnly},
		{"internal_error", "Internal error", TurnRefusalOnly},
		{"mixed_quotes_refusal", mixed, TurnSubstantive},
		{"substantive", work, TurnSubstantive},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyTurnOutput(tc.text); got != tc.want {
				t.Fatalf("ClassifyTurnOutput = %v, want %v", got, tc.want)
			}
			wantRefusal := tc.want == TurnRefusalOnly
			if got := RefusalOnlyTurn(tc.text); got != wantRefusal {
				t.Fatalf("RefusalOnlyTurn = %v, want %v", got, wantRefusal)
			}
		})
	}
}

// Over-broadness: a classifier that marks every non-empty turn as refusal
// would strand the fleet — substantive work must remain substantive.
func TestT454ClassifyTurnOutputNotOverBroad(t *testing.T) {
	if ClassifyTurnOutput("Landed the fix; GATE green.") != TurnSubstantive {
		t.Fatal("substantive work classified as non-substantive")
	}
	if ClassifyTurnOutput("") != TurnEmpty {
		t.Fatal("empty must stay empty")
	}
}
