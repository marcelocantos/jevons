// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"regexp"
	"testing"

	"github.com/marcelocantos/jevons/internal/targetfile"
)

var (
	revertedDayRe = regexp.MustCompile(`(?m)^Reverted (\d{4}-\d{2}-\d{2}):`)
	recordedOnRe  = regexp.MustCompile(`recorded the 🎯T449 owner gate on (\d{4}-\d{2}-\d{2})`)
	recordedAtRe  = regexp.MustCompile(`recorded (\d{4}-\d{2}-\d{2}) by`)
)

// TestT735OneApplyOneDate is the product-path oracle: a jevons ledger write
// and the bullseye fields it is applied with name the same calendar day.
// The 🎯T711 specimen (recorded 2026-09-20 inside a reason that said the row
// was achieved 2026-09-21) is one write, one instant, two days — this test
// fails that shape. The UTC-crossing fixture itself lives in ownergate
// (TestT735LedgerDayIsTheLocalCalendarNotUTC); here the apply is live.
func TestT735OneApplyOneDate(t *testing.T) {
	requireBullseye(t)
	s := New(t.TempDir(), nil, nil)
	repo, id := t728AchievedFixture(t)
	before, ok := targetfile.LoadGateRowFromCwd(repo, id)
	if !ok || before.Achieved == "" {
		t.Fatal("fixture has no achieved date")
	}

	res, err := s.handleOwnerGate(context.Background(), t728RecordReq(repo, id))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("record: %s", targetFileToolText(res))
	}

	row, ok := targetfile.LoadGateRowFromCwd(repo, id)
	if !ok {
		t.Fatal("row vanished after record")
	}

	reverted := revertedDayRe.FindStringSubmatch(row.Context)
	if reverted == nil {
		t.Fatalf("bullseye did not stamp Reverted <date>: in context:\n%s", row.Context)
	}
	recordedOn := recordedOnRe.FindStringSubmatch(row.Context)
	if recordedOn == nil {
		t.Fatalf("reopen reason did not name the recorded-on day:\n%s", row.Context)
	}
	if reverted[1] != recordedOn[1] {
		t.Fatalf("one apply, two dates: bullseye Reverted %s vs jevons recorded-on %s\ncontext:\n%s",
			reverted[1], recordedOn[1], row.Context)
	}
	if before.Achieved != recordedOn[1] {
		t.Fatalf("prior achieve %s disagrees with this write's recorded-on %s — the T711 two-date shape",
			before.Achieved, recordedOn[1])
	}

	recordedAt := recordedAtRe.FindStringSubmatch(row.OwnedByReason)
	if recordedAt == nil {
		t.Fatalf("gate reason did not stamp recorded <date>:\n%s", row.OwnedByReason)
	}
	if recordedAt[1] != reverted[1] {
		t.Fatalf("gate reason recorded %s vs bullseye Reverted %s in the same ceremony", recordedAt[1], reverted[1])
	}
}
