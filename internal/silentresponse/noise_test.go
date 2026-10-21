// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package silentresponse

import "testing"

// 🎯T1054 acceptance 2: a small deterministic regression set — a routine
// worker finish report (suppressed), a report carrying the previously missed
// hypothetical safety-incident phrase (passed), a real actionable incident
// (passed), a direct owner question answer (passed), and a legitimate
// duplicate/repeated message control (passed, because repetition is not a
// signal this classifier reads at all — that is relayroute/dedup's job).
func TestIsRoutineNoise_PinnedRegressionSet(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		text string
		want bool
	}{
		{
			name: "routine worker finish report restatement",
			text: "Good, jv-t900-auto landed the fix and reported green. No action needed.",
			want: true,
		},
		{
			name: "confirmed routine restatement",
			text: "Confirmed, consistent with the prior report.",
			want: true,
		},
		{
			name: "hypothetical safety incident phrase must pass through",
			// This is the exact failure mode the owner called out 2026-10-11:
			// a message that LOOKS like routine chatter but names a safety
			// incident must never be suppressed.
			text: "Good, though if this script had run unsupervised it could have been a safety incident.",
			want: false,
		},
		{
			name: "real actionable incident must pass through",
			text: "The daemon crashed during the restart and the fleet is down. Need a decision on rollback.",
			want: false,
		},
		{
			name: "direct owner question answer must pass through",
			text: "The gate id for that run is 9f13c0a2 and it came back GREEN.",
			want: false,
		},
		{
			name: "text containing a question mark is never suppressed",
			text: "Good, do you want me to proceed with the merge?",
			want: false,
		},
		{
			name: "duplicate/repeated message control is not auto-suppressed by this classifier",
			// Repetition dedup is a different concern (relayroute / send
			// dedup); this classifier only reads shape, so a repeated
			// ordinary status line is judged the same as any other.
			text: "Rebuilding the UI bundle now.",
			want: false,
		},
		{
			name: "long hedged status is left visible even with a lead-in",
			text: "Good, but there are several things worth flagging here and this message runs on for quite a while " +
				"describing multiple independent pieces of status that a human really ought to read in full before " +
				"deciding whether anything needs to happen next, so it should not be treated as routine chatter.",
			want: false,
		},
		{
			name: "empty text",
			text: "",
			want: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := IsRoutineNoise(c.text); got != c.want {
				t.Fatalf("IsRoutineNoise(%q) = %v, want %v", c.text, got, c.want)
			}
		})
	}
}

func TestIsFiltered(t *testing.T) {
	t.Parallel()
	if !IsFiltered("[silent] continued jv-x") {
		t.Fatal("explicit marker still filtered")
	}
	if !IsFiltered("Good, all workers idle, nothing to report.") {
		t.Fatal("routine noise filtered via IsFiltered")
	}
	if IsFiltered("The build is broken and I need a decision.") {
		t.Fatal("material text must not be filtered")
	}
}
