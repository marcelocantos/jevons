// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// integrationBranchRel is written inside a worker's isolated worktree when
// Ensure creates or first touches it. Integrate reads it so a landing never
// targets a shared clone that moved to a different branch after mint (🎯T955).
const integrationBranchRel = ".jevons/integration-branch"

// ErrIntegrationBranchMismatch means the shared clone's checked-out branch is
// not the integration branch recorded when the worker was minted.
var ErrIntegrationBranchMismatch = errors.New("shared clone is not on the worker's integration branch")

// RecordIntegrationBranch stores the shared clone's current branch in the
// worker worktree. Called from Ensure when a tree is created or when an older
// tree has no record yet.
func RecordIntegrationBranch(sharedClone, worktreePath, branch string) error {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		out, err := gitOut(sharedClone, "symbolic-ref", "--quiet", "--short", "HEAD")
		if err != nil {
			return fmt.Errorf("worktree: integration branch: %w", err)
		}
		branch = out
	}
	dir := filepath.Join(worktreePath, filepath.Dir(integrationBranchRel))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("worktree: integration branch dir: %w", err)
	}
	path := filepath.Join(worktreePath, integrationBranchRel)
	if err := os.WriteFile(path, []byte(branch+"\n"), 0o644); err != nil {
		return err
	}
	// Shared info/exclude keeps the mint record out of every worktree's commits.
	return excludeFromGit(sharedClone, "/.jevons/integration-branch")
}

// ReadIntegrationBranch returns the branch name recorded at mint, or empty
// when no record exists (legacy worktrees before 🎯T955).
func ReadIntegrationBranch(worktreePath string) (string, error) {
	b, err := os.ReadFile(filepath.Join(worktreePath, integrationBranchRel))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// CheckIntegrationBranch refuses a landing when the shared clone left the
// branch the worker was minted on.
func CheckIntegrationBranch(worktreePath, currentBranch string) error {
	want, err := ReadIntegrationBranch(worktreePath)
	if err != nil {
		return err
	}
	if want == "" || want == currentBranch {
		return nil
	}
	return fmt.Errorf("shared clone is on %q but this worker was minted on integration branch %q: %w",
		currentBranch, want, ErrIntegrationBranchMismatch)
}
