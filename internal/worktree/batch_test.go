// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package worktree_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/marcelocantos/jevons/internal/worktree"
)

type batchAuthority struct {
	mu       sync.Mutex
	spent    map[string]bool
	expected worktree.BatchReview
	calls    int
}

func (a *batchAuthority) RedeemAndLand(id string, r worktree.BatchReview, land func() error) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if id != "g1" || a.spent[id] || r.Repo != a.expected.Repo || r.Target != a.expected.Target || r.BaseRef != a.expected.BaseRef || r.BaseSHA != a.expected.BaseSHA || len(r.Workers) != len(a.expected.Workers) {
		return fmt.Errorf("wrong or spent grant: got %+v expected %+v spent=%v", r, a.expected, a.spent[id])
	}
	for i, w := range r.Workers {
		if w != a.expected.Workers[i] {
			return errors.New("review changed")
		}
	}
	a.spent[id] = true
	a.calls++
	return land()
}
func reviewed(t *testing.T, base string, agents ...string) *batchAuthority {
	t.Helper()
	canonical, err := filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	r := worktree.BatchReview{Repo: canonical, Target: "T1051", BaseRef: "refs/heads/master", BaseSHA: git(t, base, "rev-parse", "master")}
	for _, a := range agents {
		r.Workers = append(r.Workers, worktree.ReviewedWorker{Name: a, Ref: "refs/heads/" + worktree.BranchName(a), SHA: git(t, base, "rev-parse", worktree.BranchName(a))})
	}
	return &batchAuthority{spent: map[string]bool{}, expected: r}
}
func TestT1051BatchConflictLeavesFirstUnlandedAndGrantUnspent(t *testing.T) {
	base := sharedClone(t)
	workerCommit(t, base, "one", "a.txt", "one\n")
	workerCommit(t, base, "two", "a.txt", "two\n")
	a := reviewed(t, base, "one", "two")
	before := git(t, base, "rev-parse", "master")
	_, err := worktree.IntegrateBatch(base, "T1051", "g1", []string{"one", "two"}, a)
	if !errors.Is(err, worktree.ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
	if git(t, base, "rev-parse", "master") != before || a.calls != 0 {
		t.Fatal("partial landing or grant spent before conflict")
	}
}
func TestT1051BatchSingleAdvanceAndExactReview(t *testing.T) {
	base := sharedClone(t)
	_, one := workerCommit(t, base, "one", "a.txt", "one\n")
	_, two := workerCommit(t, base, "two", "b.txt", "two\n")
	a := reviewed(t, base, "one", "two")
	if _, err := worktree.IntegrateBatch(base, "T1051", "g1", []string{"two", "one"}, a); err == nil {
		t.Fatal("reordered batch accepted")
	}
	if a.calls != 0 {
		t.Fatal("mismatched review spent")
	}
	res, err := worktree.IntegrateBatch(base, "T1051", "g1", []string{"one", "two"}, a)
	if err != nil {
		t.Fatal(err)
	}
	if res.From == res.To || a.calls != 1 {
		t.Fatalf("landing %+v spend %d", res, a.calls)
	}
	mustReach(t, base, one)
	mustReach(t, base, two)
	if _, err := worktree.IntegrateBatch(base, "T1051", "g1", []string{"one", "two"}, a); err == nil {
		t.Fatal("grant replay accepted")
	}
}
func TestT1051BatchNoAuthorityOrChangedBase(t *testing.T) {
	base := sharedClone(t)
	workerCommit(t, base, "one", "a.txt", "one\n")
	before := git(t, base, "rev-parse", "master")
	if _, err := worktree.IntegrateBatch(base, "T1051", "g1", []string{"one"}, nil); !errors.Is(err, worktree.ErrNoAuthority) {
		t.Fatal(err)
	}
	a := reviewed(t, base, "one")
	writeFile(t, filepath.Join(base, "third"), "third\n")
	git(t, base, "add", "third")
	git(t, base, "commit", "-q", "-m", "advance")
	if _, err := worktree.IntegrateBatch(base, "T1051", "g1", []string{"one"}, a); err == nil {
		t.Fatal("stale base accepted")
	}
	if a.calls != 0 || git(t, base, "rev-parse", "master") == before {
		t.Fatal("invalid base test")
	}
	// Even a writable approval-shaped file is not an authority.
	os.WriteFile(filepath.Join(base, "approval.json"), []byte(`{"grant":"g1"}`), 0600)
	if _, err := worktree.IntegrateBatch(base, "T1051", "g1", []string{"one"}, nil); !errors.Is(err, worktree.ErrNoAuthority) {
		t.Fatal(err)
	}
}
