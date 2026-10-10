// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// BatchReview is the exact tuple the PO reviewed. It is passed to the
// daemon's redemption authority while the landing lock is held. The authority
// must durably spend its single-use grant before returning nil; an uncertain
// outcome is a refusal, not a retry of the same grant.
type BatchReview struct {
	Repo    string
	Target  string
	BaseRef string
	BaseSHA string
	Workers []ReviewedWorker
}
type ReviewedWorker struct{ Name, Ref, SHA string }

// Redeemer is the daemon-controlled, durable grant authority. A nil authority
// never permits landing. This interface deliberately does not implement or
// replicate T1050's epoch store: the authority must check hold epoch and
// release under its own durable transaction.
type Redeemer interface {
	RedeemAndLand(grantID string, review BatchReview, land func() error) error
}

var ErrNoAuthority = errors.New("integration requires a running grant authority")
var ErrReviewChanged = errors.New("reviewed integration tuple changed")

// IntegrateBatch precomputes every worker merge without changing the shared
// ref, then redeems the entire exact tuple and advances the ref ONCE. A
// conflict in even the last worker cannot partially land earlier workers.
// Redemption occurs under the same landing lock as the final merge. This is
// a workflow gate, NOT protection against a worker able to write .git directly.
func IntegrateBatch(base, target, grantID string, agents []string, authority Redeemer) (*IntegrateResult, error) {
	if authority == nil {
		return nil, ErrNoAuthority
	}
	if target == "" || grantID == "" || len(agents) == 0 {
		return nil, fmt.Errorf("integrate: grant, target and workers required")
	}
	if !IsGitRepo(base) || IsLinkedWorktree(base) {
		return nil, fmt.Errorf("integrate: shared clone required")
	}
	branch, err := gitOut(base, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("integrate: shared clone is detached: %w", err)
	}
	unlock, err := lockLandings(base)
	if err != nil {
		return nil, err
	}
	defer unlock()
	repo, err := gitOut(base, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	baseTip, err := gitOut(base, "rev-parse", "--verify", "refs/heads/"+branch)
	if err != nil {
		return nil, err
	}
	review := BatchReview{Repo: repo, Target: target, BaseRef: "refs/heads/" + branch, BaseSHA: baseTip}
	res := &IntegrateResult{BaseBranch: branch, From: baseTip, To: baseTip, Attempts: 1}
	merged := baseTip
	seen := make(map[string]bool)
	for _, agent := range agents {
		if agent == "" || seen[agent] {
			return nil, fmt.Errorf("integrate: empty or duplicate worker %q", agent)
		}
		seen[agent] = true
		path := WorktreePath(base, agent)
		ref := "refs/heads/" + BranchName(agent)
		if st, e := os.Stat(path); e != nil || !st.IsDir() {
			return nil, fmt.Errorf("integrate: %s: %w", agent, ErrNotIsolated)
		}
		if got, e := gitOut(path, "symbolic-ref", "--quiet", "--short", "HEAD"); e != nil || got != BranchName(agent) {
			return nil, fmt.Errorf("integrate: %s: %w", agent, ErrNotIsolated)
		}
		if dirty, e := gitOut(path, "status", "--porcelain", "--untracked-files=no"); e != nil || dirty != "" {
			return nil, fmt.Errorf("integrate: %s: %s: %w", agent, dirty, ErrWorkerDirty)
		}
		tip, e := gitOut(base, "rev-parse", "--verify", ref)
		if e != nil {
			return nil, e
		}
		review.Workers = append(review.Workers, ReviewedWorker{Name: agent, Ref: ref, SHA: tip})
		if isAncestor(base, tip, merged) {
			continue
		}
		landed, e := gitOut(base, "rev-list", merged+".."+tip)
		if e != nil {
			return nil, e
		}
		res.Landed = append(res.Landed, strings.Fields(landed)...)
		if isAncestor(base, merged, tip) {
			merged = tip
			continue
		}
		out, code, e := gitRun(base, "merge-tree", "--write-tree", "--name-only", "--no-messages", merged, tip)
		if e != nil && code != 1 {
			return nil, fmt.Errorf("integrate: merge-tree: %w: %s", e, out)
		}
		lines := strings.Split(out, "\n")
		if code == 1 {
			return nil, fmt.Errorf("integrate: %s conflicts: %s: %w", agent, strings.Join(lines[1:], "\n"), ErrConflict)
		}
		merge, e := gitOut(base, "commit-tree", lines[0], "-p", merged, "-p", tip, "-m", fmt.Sprintf("Merge branch '%s' into %s", BranchName(agent), branch))
		if e != nil {
			return nil, e
		}
		merged = merge
		res.Merge = merge
	}
	// Re-resolve every reviewed ref, including the destination. A local ref
	// writer not taking this lock may race afterward; the --ff-only merge will
	// refuse its changed base, but OS confinement is still required for a
	// security boundary against direct writes.
	if now, e := gitOut(base, "rev-parse", "--verify", review.BaseRef); e != nil || now != baseTip {
		return nil, ErrReviewChanged
	}
	for _, w := range review.Workers {
		if now, e := gitOut(base, "rev-parse", "--verify", w.Ref); e != nil || now != w.SHA {
			return nil, ErrReviewChanged
		}
	}
	// The epoch authority must keep its target lock THROUGH the final ref
	// mutation. A hold accepted after spend but before merge must wait: it
	// cannot return accepted while the older landing remains in flight.
	err = authority.RedeemAndLand(grantID, review, func() error {
		if merged == baseTip {
			return nil
		}
		out, _, e := gitRun(base, "merge", "--ff-only", "--quiet", merged)
		if e != nil {
			return fmt.Errorf("grant spent; landing uncertain/refused (new approval required): %w: %s", e, out)
		}
		res.To = merged
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("integrate: grant refused or landing failed: %w", err)
	}
	return res, nil
}
