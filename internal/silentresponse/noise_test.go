// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package silentresponse

import (
	"strings"
	"testing"
)

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

// 🎯T1054: ClassifyLive bounds how long the live stream may hold before
// committing to a verdict, and never permanently mis-suppresses text whose
// escape word streams in after a routine-sounding opener.
func TestClassifyLive(t *testing.T) {
	t.Parallel()
	// Short routine text, streamed one fragment at a time, terminal at end:
	// Pending while short, Silent once sealed.
	if got := ClassifyLive("Good,", false); got != Pending {
		t.Fatalf("short partial = %v, want Pending", got)
	}
	if got := ClassifyLive("Good, all idle.", false); got != Pending {
		t.Fatalf("still short, not terminal = %v, want Pending", got)
	}
	if got := ClassifyLive("Good, all idle.", true); got != Silent {
		t.Fatalf("sealed routine = %v, want Silent", got)
	}

	// The load-bearing case: a routine-looking opener that later reveals a
	// safety/incident escape word must NOT be silenced, even though early
	// fragments alone looked identical to the routine case above.
	if got := ClassifyLive("Good,", false); got != Pending {
		t.Fatalf("opener alone = %v, want Pending", got)
	}
	full := "Good, though if this script had run unsupervised it could have been a safety incident."
	if got := ClassifyLive(full, true); got != Visible {
		t.Fatalf("sealed with late escape word = %v, want Visible", got)
	}

	// A long, substantive answer must not be held for the whole turn: once
	// accumulated text exceeds NoiseBufferRunes it commits to Visible
	// immediately, before terminal, so a real answer streams live.
	long := strings.Repeat("x", NoiseBufferRunes+1)
	if got := ClassifyLive(long, false); got != Visible {
		t.Fatalf("over-cap non-terminal = %v, want Visible", got)
	}

	// The explicit [silent] marker still commits immediately, not bounded by
	// terminal/length — same behaviour as before this change.
	if got := ClassifyLive("[silent] continuing", false); got != Silent {
		t.Fatalf("explicit marker mid-stream = %v, want Silent", got)
	}

	// Empty text: Pending while streaming, Visible if sealed empty (terminal
	// end_turn with nothing accumulated — e.g. a tool-only turn).
	if got := ClassifyLive("", false); got != Pending {
		t.Fatalf("empty non-terminal = %v, want Pending", got)
	}
	if got := ClassifyLive("", true); got != Visible {
		t.Fatalf("empty terminal = %v, want Visible", got)
	}
}
