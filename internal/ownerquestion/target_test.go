package ownerquestion

import (
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/ownergate"
	"github.com/marcelocantos/jevons/internal/targetfile"
)

func TestFromTargetLedgerT449ReasonPages(t *testing.T) {
	repo := t.TempDir()
	reason, err := ownergate.Record{Question: "Does the aside selection stay put after a hard reload?", Evidence: "commit abcdef0123456789 GATE id=deadbeef", RecordedBy: "jevons-po"}.Reason()
	if err != nil {
		t.Fatal(err)
	}
	row := targetfile.GateRow{ID: "T383", Status: "converging", OwnedByOwner: ownergate.OwnerHandle, OwnedByReason: reason}
	q, _, ok, err := FromTargetLedger(row, repo)
	if err != nil || !ok || q.Identity.ID != "explicit-owner-blocker" || !strings.Contains(q.Text, "hard reload") {
		t.Fatalf("ledger gate %+v ok=%v err=%v", q, ok, err)
	}
}

func TestFromTargetLedgerDeviceKeypressDoesNotPage(t *testing.T) {
	repo := t.TempDir()
	row := targetfile.GateRow{ID: "T370", Status: "converging", OwnedByOwner: "marcelo", OwnedByReason: "needs real cmd-shift-] keypress"}
	if _, _, ok, _ := FromTargetLedger(row, repo); ok {
		t.Fatal("device-only hold paged")
	}
}

func TestFromTargetLedgerExplicitContextWithoutOwnedBy(t *testing.T) {
	repo := t.TempDir()
	ctx := "owner decision requested: choose cube-map A or B for YourWorld2. Evidence: commit fedcba0987654321."
	row := targetfile.GateRow{ID: "T44.6", Status: "converging", Context: ctx}
	q, _, ok, err := FromTargetLedger(row, repo)
	if err != nil || !ok || !strings.Contains(q.Text, "cube-map") {
		t.Fatalf("context explicit ask %+v ok=%v err=%v", q, ok, err)
	}
}
