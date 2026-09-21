package poproactive

import "testing"

func TestT753PendingVerificationIsNotReady(t *testing.T) {
	leaf := LeafObs{ID: "T753", ReapedPending: "jevons-po owes verification of abc1234"}
	if got := ClassifyLeaf(leaf); got != LeafSkipReapedPending {
		t.Fatalf("kind=%v", got)
	}
	if d := Classify([]LeafObs{leaf}); d.Mode != Sleep || len(d.ReadyIDs) != 0 {
		t.Fatalf("decision=%+v", d)
	}
	leaf.Closed = true
	if got := ClassifyLeaf(leaf); got != LeafSkipClosed {
		t.Fatalf("closed kind=%v", got)
	}
}
