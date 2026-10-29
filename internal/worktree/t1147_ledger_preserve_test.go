// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/commitscope"
	"github.com/marcelocantos/jevons/internal/worktree"
)

// 🎯T1147 hermetic: an uncommitted achieve survives integrate fast-forward and
// a ledger restore merge rather than a wholesale HEAD replace.
func TestT1147UncommittedAchieveSurvivesIntegrateAndRestore(t *testing.T) {
	base := sharedClone(t)
	seed := `schema_version: 1
targets:
  T1: {name: seed, status: identified}
`
	writeFile(t, filepath.Join(base, "bullseye.yaml"), seed)
	git(t, base, "add", "bullseye.yaml")
	git(t, base, "commit", "-q", "-m", "seed ledger")
	head := readFile(t, filepath.Join(base, "bullseye.yaml"))

	achieved := `schema_version: 1
targets:
  T1: {name: seed, status: identified}
  T1147: {name: preserve me, status: achieved}
`
	writeFile(t, filepath.Join(base, "bullseye.yaml"), achieved)

	_, sha := workerCommit(t, base, "jv-t1147", "land.txt", "land\n")
	if _, err := worktree.Integrate(&worktree.IntegrateArgs{BaseWorkdir: base, AgentName: "jv-t1147"}); err != nil {
		t.Fatalf("Integrate: %v", err)
	}
	mustReach(t, base, sha)
	got := readFile(t, filepath.Join(base, "bullseye.yaml"))
	if !strings.Contains(got, "T1147:") || !strings.Contains(got, "achieved") {
		t.Fatalf("integrate dropped uncommitted achieve:\n%s", got)
	}

	working := readFile(t, filepath.Join(base, "bullseye.yaml"))
	restored, err := commitscope.UnionPreservingWorkingDivergence([]byte(head), []byte(working), []byte(head))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(restored), "T1147") {
		t.Fatalf("restore-to-HEAD merge dropped achieve:\n%s", restored)
	}
}

func TestRestoreLedgerFromCommitPreservesWorkingAchieve(t *testing.T) {
	base := sharedClone(t)
	seed := `schema_version: 1
targets:
  T1: {name: seed, status: identified}
`
	writeFile(t, filepath.Join(base, "bullseye.yaml"), seed)
	git(t, base, "add", "bullseye.yaml")
	git(t, base, "commit", "-q", "-m", "seed")
	older := git(t, base, "rev-parse", "HEAD")

	achieved := `schema_version: 1
targets:
  T1: {name: seed, status: identified}
  T1147: {name: preserve me, status: achieved}
`
	writeFile(t, filepath.Join(base, "bullseye.yaml"), achieved)
	git(t, base, "add", "bullseye.yaml")
	git(t, base, "commit", "-q", "-m", "achieve")
	git(t, base, "reset", "--hard", older)
	writeFile(t, filepath.Join(base, "bullseye.yaml"), achieved)

	if err := worktree.RestoreLedgerFromCommit(base, older); err != nil {
		t.Fatal(err)
	}
	final := readFile(t, filepath.Join(base, "bullseye.yaml"))
	if !strings.Contains(final, "T1147:") {
		t.Fatalf("restore dropped working achieve:\n%s", final)
	}
}
