// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitscope_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/commitscope"
)

// 🎯T1144: the 2026-10-21/22 merge truncations were ~100KiB working-tree
// clobbers (blob sizes 100208/100234), not bullseye emitting a canonical
// 27-target ledger. The specimen drops thousands of rows versus HEAD.
func TestT1144IncidentTruncatedLedgerSpecimenRefused(t *testing.T) {
	trunc, err := exec.Command("git", "show", "08a69a88:bullseye.yaml").Output()
	if err != nil {
		t.Skip("incident commit 08a69a88 not in this clone")
	}
	full, err := exec.Command("git", "show", "08a69a88^1:bullseye.yaml").Output()
	if err != nil {
		t.Fatalf("parent ledger: %v", err)
	}
	refuse, msg := commitscope.LedgerShrinkRefusal(full, trunc)
	if !refuse {
		t.Fatal("incident truncated ledger must be refused versus full parent")
	}
	if !strings.Contains(msg, "T1014") && !strings.Contains(msg, "Lost target rows") {
		t.Fatalf("refusal should name lost rows from incident specimen: %q", msg)
	}
}
