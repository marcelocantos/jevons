// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import "testing"

// 🎯T493.1: after visual cockpit work the agent answers in prose whether
// the screenshot looks like a normal transcript. A green metric is not
// that answer.
func TestClassifyVisualVerdict(t *testing.T) {
	const goodYes = "" +
		"Done. Viewport screenshot of #messages after hard reload: five assistant " +
		"bubbles with readable body text packed to the bottom. Empty canvas: none — " +
		"the last bubble sits at the live end. Latest: hidden. Does this look like " +
		"a normal chat transcript after a hard reload? Yes."
	const goodNo = "" +
		"Not achieving. Ink: one leftover assistant bubble at the top of a tall pane. " +
		"Empty: most of the viewport is desert turn-slots. Latest: showing. Does this " +
		"look like a normal chat transcript after a hard reload? No."
	const incident = "" +
		"T494 achieved. visibleInScroller=1, modelRows=328. Screenshot /tmp/t494.png. " +
		"GATE abc GREEN. Screenshot caption: A chat interface showing a conversation."
	const yesAgainstNo = "" +
		"Done. Ink: one leftover bubble in a tall pane. Empty: desert of empty slots. " +
		"Latest: showing. Does this look like a normal chat transcript after a hard reload? Yes."
	const falseGreen = "" +
		"Achieved. Ink: one leftover assistant bubble. Empty: desert. Latest: showing. " +
		"Does this look like a normal chat transcript after a hard reload? No. J19 green."
	const noAndFix = "" +
		"Ink: leftover bubble. Empty: desert. Latest: showing. Does this look like " +
		"a normal chat transcript after a hard reload? No. J19 is green — false green; " +
		"fixing the oracle in this turn. Not done."
	cases := []struct {
		name string
		in   string
		want VisualVerdictClass
	}{
		{"empty", "", VisualVerdictNotApplicable},
		{"daemon daily-path", "Done. restart-daily-jevonsd + curl :13705/api/frontier HTTP 200. SHA deadbeef", VisualVerdictNotApplicable},
		{"in progress cockpit", "in progress: reading virtualizeMessages pin path in #messages", VisualVerdictNotApplicable},
		{"incident metric as verdict", incident, VisualVerdictMissing},
		{"caption as verdict", "Done. #messages screenshot caption: The image shows a chat window.", VisualVerdictMissing},
		{"done without look", "Done. pinToLiveEnd + virtualizeMessages. go test ./web green.", VisualVerdictMissing},
		{"good yes", goodYes, VisualVerdictPresent},
		{"good no stop", goodNo, VisualVerdictNotApplicable},
		{"yes against automatic no", yesAgainstNo, VisualVerdictYesAgainstNo},
		{"no plus green journey", falseGreen, VisualVerdictFalseGreen},
		{"no plus false-green fix", noAndFix, VisualVerdictPresent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyVisualVerdict(tc.in)
			if got != tc.want {
				t.Fatalf("ClassifyVisualVerdict(%q)=%s want %s", tc.in, got, tc.want)
			}
		})
	}
}

func TestHasVisualProseVerdict(t *testing.T) {
	good := "Ink: five bubbles. Empty: packed, no desert. Latest: hidden. Does this look like a normal chat transcript after a hard reload? Yes."
	if !HasVisualProseVerdict(good) {
		t.Fatal("expected four-part prose to count")
	}
	if HasVisualProseVerdict("visibleInScroller=5 modelRows=313 Latest hidden") {
		t.Fatal("census fields are not a prose verdict")
	}
	if HasVisualProseVerdict("screenshot caption: A chat interface showing messages") {
		t.Fatal("caption is not a prose verdict")
	}
}

func TestLooksLikeMissingVisualVerdict(t *testing.T) {
	if !LooksLikeMissingVisualVerdict("Achieved. visibleInScroller=1 on #messages. GATE x GREEN.") {
		t.Fatal("incident shape must flag missing visual verdict")
	}
	if LooksLikeMissingVisualVerdict("still looking at the #messages pin") {
		t.Fatal("in-progress must not flag")
	}
	good := "Done. Ink: two user bubbles and three assistant bubbles filling the pane. Empty: none. Latest: hidden. Does this look like a normal chat transcript after a hard reload? Yes."
	if LooksLikeMissingVisualVerdict(good) {
		t.Fatal("four-part look must not flag missing")
	}
}

func TestLooksLikeAutomaticVisualNo(t *testing.T) {
	if !LooksLikeAutomaticVisualNo("leftover bubble, Latest showing, desert pane") {
		t.Fatal("incident picture is automatic no")
	}
	if LooksLikeAutomaticVisualNo("Latest: hidden. Empty canvas: none. packed bubbles.") {
		t.Fatal("packed pane with Latest hidden is not automatic no")
	}
}

func TestLooksLikeYesAgainstAutomaticNo(t *testing.T) {
	in := "Done. Ink: leftover bubble. Empty: desert. Latest: showing. Does this look like a normal chat transcript after a hard reload? Yes."
	if !LooksLikeYesAgainstAutomaticNo(in) {
		t.Fatal("yes against leftover+Latest+desert must flag")
	}
}

func TestLooksLikeFalseGreenVisualJourney(t *testing.T) {
	in := "Done. Ink: leftover. Empty: desert. Latest: showing. Does this look like a normal chat transcript after a hard reload? No. J19 green."
	if !LooksLikeFalseGreenVisualJourney(in) {
		t.Fatal("no + green J19 treated as done must flag")
	}
	fix := "Done is refused. Ink: leftover. Empty: desert. Latest: showing. Does this look like a normal chat transcript after a hard reload? No. J19 green is a false green; fixing the oracle."
	if LooksLikeFalseGreenVisualJourney(fix) {
		t.Fatal("naming the false green and fixing the oracle is the required path")
	}
}

func TestVisualVerdictClassString(t *testing.T) {
	if VisualVerdictMissing.String() != "missing" {
		t.Fatalf("got %q", VisualVerdictMissing.String())
	}
	if VisualVerdictPresent.String() != "present" {
		t.Fatalf("got %q", VisualVerdictPresent.String())
	}
	if VisualVerdictNotApplicable.String() != "not_applicable" {
		t.Fatalf("got %q", VisualVerdictNotApplicable.String())
	}
}
