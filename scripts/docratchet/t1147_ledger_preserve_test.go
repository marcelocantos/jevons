// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

func TestT1147LedgerPreserveSeams(t *testing.T) {
	needles := []struct {
		rel    string
		substr string
	}{
		{"internal/commitscope/ledger_preserve.go", "T1147"},
		{"internal/worktree/ledger_restore.go", "RestoreLedgerFromCommit"},
		{"internal/ledgercommit/autocommit.go", "CommitDirtyLedger"},
		{"internal/treeguard/hook.go", "T1147"},
		{"cmd/ledgercommit/main.go", "ledgercommit"},
	}
	for _, n := range needles {
		if !strings.Contains(readRepo(t, n.rel), n.substr) {
			t.Fatalf("%s missing %q", n.rel, n.substr)
		}
	}
}
