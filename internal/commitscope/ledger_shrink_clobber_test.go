// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitscope_test

import (
	"os/exec"
	"testing"

	"github.com/marcelocantos/jevons/internal/commitscope"
)

// 🎯T1144: both 2026-10-21/22 merges had full parents but landed ~100KiB working-
// tree clobbers (blob sizes 100208/100234), not a bullseye canonical 27-target
// export. Truncating the parent at the incident byte length reproduces the
// specimen census; the shrink guard must refuse so the full ledger survives.
func TestT1144IncidentByteTruncateReproducesClobberRefused(t *testing.T) {
	full, err := exec.Command("git", "show", "08a69a88^1:bullseye.yaml").Output()
	if err != nil {
		t.Skip("incident parent not in this clone")
	}
	incident, err := exec.Command("git", "show", "08a69a88:bullseye.yaml").Output()
	if err != nil {
		t.Fatalf("incident blob: %v", err)
	}
	if len(incident) != 100208 {
		t.Fatalf("incident blob length = %d, want 100208", len(incident))
	}
	if commitscope.CountLedgerTargetKeys(incident) != 27 {
		t.Fatalf("incident key census = %d, want 27", commitscope.CountLedgerTargetKeys(incident))
	}
	clobber := full[:len(incident)]

	refuse, msg := commitscope.LedgerShrinkRefusal(full, clobber)
	if !refuse {
		t.Fatal("partial-byte clobber must be refused versus full parent")
	}
	if msg == "" {
		t.Fatal("refusal should name lost rows")
	}
}
