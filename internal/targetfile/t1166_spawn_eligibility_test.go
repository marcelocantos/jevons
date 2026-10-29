// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package targetfile

import (
	"path/filepath"
	"testing"
)

// 🎯T1166: kickoff status must follow the canonical repository ledger, not a
// stale bullseye.yaml left in a linked worktree after achieve→revert on the
// shared clone.
func TestT1166LoadTargetStatusUsesCanonicalLedgerNotStaleWorktreeCopy(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	clone, wt := t1105FakeWorktree(t, root, "jevons", "jv-t1166-spawn-eligibility-cache")
	const id = "T1166"
	t1105Write(t, filepath.Join(clone, "bullseye.yaml"), `targets:
  `+id+`:
    name: reverted on shared clone
    status: converging
`)
	t1105Write(t, filepath.Join(wt, "bullseye.yaml"), `targets:
  `+id+`:
    name: stale achieved copy in worktree checkout
    status: achieved
`)
	st, ok := LoadTargetStatusFromCwd(wt, id)
	if !ok {
		t.Fatal("status must be found via canonical ledger")
	}
	if st != "converging" {
		t.Fatalf("worktree cwd must not read stale achieved copy: status=%q", st)
	}
	if IsClosedStatus(st) {
		t.Fatal("reverted target must be open for kickoff")
	}
}
