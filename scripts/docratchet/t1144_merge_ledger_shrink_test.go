// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

// 🎯T1144: merge commits and integrate landings must refuse ledger shrink.
func TestT1144MergeLedgerShrinkRatchet(t *testing.T) {
	install := readRepo(t, "cmd/commitscope/main.go")
	if !strings.Contains(install, `"pre-merge-commit"`) {
		t.Fatal("commitscope --install must install pre-merge-commit (🎯T1144)")
	}
	hook := readRepo(t, "scripts/hooks/pre-merge-commit")
	if !strings.Contains(hook, "🎯T1144") {
		t.Fatal("scripts/hooks/pre-merge-commit must carry 🎯T1144 marker")
	}
	integrate := readRepo(t, "internal/worktree/integrate.go")
	if !strings.Contains(integrate, "refuseLandingLedgerShrink") {
		t.Fatal("integrate must check ledger shrink before fast-forward (🎯T1144)")
	}
	shrink := readRepo(t, "internal/commitscope/ledger_shrink.go")
	if !strings.Contains(hook, "JEVONS_PRE_MERGE_COMMIT") {
		t.Fatal("pre-merge-commit must set JEVONS_PRE_MERGE_COMMIT (🎯T1144)")
	}
	if !strings.Contains(shrink, "Lost target rows") {
		t.Fatal("ledger shrink refusal must name lost rows (🎯T1144)")
	}
}
