// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/marcelocantos/jevons/internal/commitscope"
	"github.com/marcelocantos/jevons/internal/ledgercommit"
)

// RestoreLedgerFromCommit rewrites the working-tree ledger from sourceRev,
// starting from the latest full ledger (HEAD plus any uncommitted rows) rather
// than replacing wholesale (🎯T1147).
func RestoreLedgerFromCommit(repoRoot, sourceRev string) error {
	head, err := gitBlob(repoRoot, "HEAD:"+ledgerPath)
	if err != nil {
		return fmt.Errorf("restore ledger: HEAD: %w", err)
	}
	source, err := gitBlob(repoRoot, sourceRev+":"+ledgerPath)
	if err != nil {
		return fmt.Errorf("restore ledger: %s: %w", sourceRev, err)
	}
	path := filepath.Join(repoRoot, ledgerPath)
	working, _ := os.ReadFile(path)
	candidate, err := commitscope.UnionLedgerYAML(head, source)
	if err != nil {
		return err
	}
	_, err = ledgercommit.WriteMergedLedger(repoRoot, head, working, candidate)
	return err
}
