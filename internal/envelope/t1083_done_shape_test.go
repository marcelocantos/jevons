// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package envelope

import (
	"strings"
	"testing"
)

// t1083Report is the 2026-10-20 pilot shape: a read-only worker's
// finish-report carrying the HEAD it measured, with status set to an English
// completion word. statusLine "" omits the slot.
func t1083Report(statusLine string) string {
	var b strings.Builder
	b.WriteString("```jevons\n")
	b.WriteString("jevons: kind finish-report\n")
	b.WriteString("jevons: target T1083\n")
	if statusLine != "" {
		b.WriteString("jevons: status " + statusLine + "\n")
	}
	b.WriteString("jevons: sha 31134215\n")
	b.WriteString("jevons: silent-ledger none\n")
	b.WriteString("```\n\nCounted lines at HEAD; nothing committed.")
	return b.String()
}

// TestT1083StatusDoneIsUnknownWithNamedSlotNotice: a status outside the
// enum is not read as done, and the author is told which slot failed and
// every value it accepts.
func TestT1083StatusDoneIsUnknownWithNamedSlotNotice(t *testing.T) {
	for _, word := range []string{"done", "achieved", "complete"} {
		report := t1083Report(word)
		if got := ClassifyDone(report); got != DoneUnknown {
			t.Fatalf("status %s: ClassifyDone=%s, want unknown", word, got)
		}
		_, err := Parse(report)
		se, ok := AsSlotError(err)
		if !ok || se.Slot != "status" || se.Value != word {
			t.Fatalf("status %s: want a status SlotError, got %v", word, err)
		}
		notice := CorrectionNotice(report)
		if notice == "" {
			t.Fatalf("status %s: no correction notice", word)
		}
		for _, want := range []string{
			"`jevons: status " + word + "`",
			"in-progress | live | landed | shipped | blocked",
			"omits status",
			"Resend a corrected finish-report",
			DoneShape,
		} {
			if !strings.Contains(notice, want) {
				t.Errorf("status %s: notice missing %q:\n%s", word, want, notice)
			}
		}
		if strings.Contains(notice, "confidence") {
			t.Errorf("status %s: notice carries the unrelated T793 confidence reminder:\n%s", word, notice)
		}
	}
}

// TestT1083StatusOmittedWithSHAIsDone: the same report with status omitted
// is the done shape.
func TestT1083StatusOmittedWithSHAIsDone(t *testing.T) {
	report := t1083Report("")
	if got := ClassifyDone(report); got != DoneYes {
		_, err := Parse(report)
		t.Fatalf("ClassifyDone=%s (err %v), want yes", got, err)
	}
	if n := CorrectionNotice(report); n != "" {
		t.Fatalf("done shape drew a notice: %s", n)
	}
}

// TestT1083DoneShapeIsDone holds the published shape to the classifier.
func TestT1083DoneShapeIsDone(t *testing.T) {
	if got := ClassifyDone(DoneShape + "\n\nSurveyed; nothing committed."); got != DoneYes {
		_, err := Parse(DoneShape)
		t.Fatalf("DoneShape classifies %s (err %v)", got, err)
	}
	m, _ := Parse(DoneShape)
	if m.Status != ProgressNone {
		t.Fatalf("DoneShape declares status %q", m.Status)
	}
}

// TestT1083NoStatusValueIsDone: the reason the done shape omits status.
// in-progress and blocked are not done; live/landed/shipped are product
// claims. Bare status values never turn a non-oracle report into done.
func TestT1083NoStatusValueIsDone(t *testing.T) {
	for _, p := range ProgressValues() {
		if _, ok := ParseProgress(string(p)); !ok {
			t.Fatalf("ProgressValues lists %q, which ParseProgress refuses", p)
		}
		report := t1083Report(string(p))
		got := ClassifyDone(report)
		switch p {
		case ProgressInProgress, ProgressBlocked:
			if got == DoneYes {
				t.Errorf("status %s classifies done", p)
			}
		case ProgressLive, ProgressLanded, ProgressShipped:
			if got != DoneYes {
				t.Errorf("status %s with an oracle classifies %s", p, got)
			}
			if !p.ProductVisible() {
				t.Errorf("status %s is not product-visible", p)
			}
		default:
			t.Errorf("ProgressValues grew %q; decide its done class here", p)
		}
	}
}

// TestT1083ClosedVocabularySlotErrorsListValues: verdict, risk and kind
// misses name their accepted values too.
func TestT1083ClosedVocabularySlotErrorsListValues(t *testing.T) {
	cases := []struct{ line, slot, want string }{
		{"jevons: verdict PASS", "verdict", "GREEN | SUSPECT"},
		{"jevons: risk low", "risk", "none | class-3 | residual | accepted-risk"},
	}
	for _, c := range cases {
		report := strings.Replace(t1083Report(""), "jevons: sha 31134215", "jevons: sha 31134215\n"+c.line, 1)
		_, err := Parse(report)
		se, ok := AsSlotError(err)
		if !ok || se.Slot != c.slot || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err=%v, want %s SlotError listing %q", c.line, err, c.slot, c.want)
		}
	}
	if _, err := Parse("```jevons\njevons: kind finished\n```"); err == nil ||
		!strings.Contains(err.Error(), "finish-report | status-ping") {
		t.Errorf("unknown kind err=%v, want accepted kinds listed", err)
	}
}
