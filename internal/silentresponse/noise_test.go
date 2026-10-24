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

// 🎯T1054 acceptance 2: precision report on real session samples.
//
// 20 real overseer/PO assistant messages pulled from actual jevons fleet
// sessions (via mnemo search on repo=jevons), each hand-labelled with
// ground truth: should the owner see this, or is it routine no-action
// chatter? Text is quoted verbatim (occasionally truncated where the
// source snippet itself was truncated) — nothing here is synthetic.
//
// Sources (session id, approx date): 019fc712-...-6961, 019fc1ba-...-2ab3,
// 019fd105-...-42dbb, 019fe0d9-...-fd16, 019fbaf1-...-4ea13,
// 019fd13d-...-50e21, 019fe341-...-58ca2, 01a00f1f-...-431f6 — all
// marcelocantos/jevons, 2026-08 through 2026-08-18.
func TestIsFiltered_RealSessionPrecisionReport(t *testing.T) {
	t.Parallel()
	type sample struct {
		text     string
		ownerSees bool // ground truth: true = must stay visible
	}
	samples := []sample{
		{"Hermetic oracle green. Committing and notifying T188 + overseer.", true},
		{"## Confirmed  | | | |--|--| | **SHA** | `043eb1b` | | **Glyphs** | collapsed → ▾, expanded → ▴ | | **Size** | `.chev` 10px → 13px | | **Oracle** | collapse-test", true},
		{"silent - already achieved", false},
		{"Aligned.  - No mass-spawn   - **`jv-t346-sentinel-frontier`** in progress on **🎯T346**   - PO idle on implement (correct)    I'll gate T346 when the finish repo", true},
		{"Locked on **🎯T65**.  **Acceptance bar (PO):** prefix-first (`aside: …` etc.); **reject** button-primary Capture/Aside/Main; stack/RHS OK if quiet; prefix cheat-", true},
		{"## Confirmed — 🎯T106 done (Build plane)  | Check | Result | |--------|--------| | Product SHA | `ff7c5ee` on local master | | Ledger | **achieved** + attestatio", true},
		{"**Spawned** `jv-t247-aside-immediate` — `target_id=T247`, brief delivered.  **Children under jevons-po:** `jv-t246-msg-expand` (T246) + `jv-t247-aside-immediate", true},
		{"**Folded into `jv-t173-frontier-chrome`** (interrupt attempted; brief queued if turn was mid-flight):  - ID / status / fanout → shrink-wrap + light padding   -", true},
		{"[silent] T273 already gated (bf009e9); jv-t273 duplicate finish ack — no rework.", false},
		{"[silent] Independent PO gate 🎯T244: SHA ca5481c on master; re-ran hermetics PASS; /health+/api/frontier 200; ledger already achieved; worker reaped; zero childr", false},
		{"`jv-t496-owner-reply` is registered and **running**, `target_id=T496`, but **phase=idle** after the overseer CONTINUE. Transcript shows it started (prod at chat", true},
		{"Two small updates, both waiting.  - **`jv-t625-journey-triage`:** the expiry notice was for its old J13 monitor, already resolved. J30 is still running and its", true},
		{"`jv-t819-fold-md-clip` is still waiting on its J23 gate and hasn't chosen a side. It notes the overseer `chatlog` is still the replay source, which weakens the", true},
		{"Acknowledged — voice as I/O, agent elsewhere. Writing it up as a target.", true},
		{"Updating the stale tasks — T18's pending phases are superseded by T25's direction.", true},
		{"No response requested.", false}, // 🎯T502 turn-boundary artifact; a different mechanism already drops this one, this classifier is not expected to catch it
		{"Clean report for the user.", true},
		{"`jv-t285.2-icon-menu` — Claude, **working** on the manual provider/model menu.  Also Claude/working: T502, T412, T433, T503 (the", true},
		{"T25's acceptance is written around the now-dead Go server-side voice bridge. The architecture moved fully client-side. Rewriting it to the real plan, recording", true},
		{"Honestly — you're right. The hacky feeling isn't tooling fatigue, it's a mismatch between the API design and our use case.  **What xAI Realtime is actually for:", true},
	}
	var tp, fp, tn, fn int
	for _, s := range samples {
		suppressed := IsFiltered(s.text)
		switch {
		case suppressed && !s.ownerSees:
			tp++
		case suppressed && s.ownerSees:
			fp++
			t.Errorf("FALSE POSITIVE (unsafe): suppressed a message the owner should see: %q", s.text)
		case !suppressed && !s.ownerSees:
			fn++
		case !suppressed && s.ownerSees:
			tn++
		}
	}
	total := tp + fp + tn + fn
	precision := 1.0
	if tp+fp > 0 {
		precision = float64(tp) / float64(tp+fp)
	}
	t.Logf("🎯T1054 precision report over %d real session samples: TP=%d FP=%d TN=%d FN=%d precision=%.2f",
		total, tp, fp, tn, fn, precision)
	// The safety property this classifier must hold: it never suppresses a
	// message the owner should see. Recall (catching every routine phrasing)
	// is deliberately NOT asserted here — the lead-in wordlist is narrow by
	// design, so some routine chatter phrased outside it is correctly missed
	// (false negative, the safe failure direction) rather than risk a false
	// positive.
	if fp != 0 {
		t.Fatalf("got %d false positives, want 0 (never suppress owner-visible content)", fp)
	}
}
