// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package ledgercommit lands a scoped bullseye.yaml commit promptly after a
// mutation (🎯T1147 acceptance path A).
package ledgercommit

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/marcelocantos/jevons/internal/commitscope"
)

// CommitDirtyLedger runs `git commit --only bullseye.yaml` when the ledger
// differs from HEAD. No-op when already clean. message is the commit subject.
func CommitDirtyLedger(repoRoot, message string) error {
	repoRoot = filepath.Clean(repoRoot)
	rel := commitscope.SharedLedger
	if err := exec.Command("git", "-C", repoRoot, "diff", "--quiet", "HEAD", "--", rel).Run(); err == nil {
		return nil
	}
	if message == "" {
		message = "ledger: bullseye write"
	}
	cmd := exec.Command("git", "-C", repoRoot, "commit", "--only", rel, "-m", message)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ledgercommit: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// RepoRoot is the git toplevel for dir.
func RepoRoot(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// CommitCwd is CommitDirtyLedger after resolving repoRoot from cwd.
func CommitCwd(cwd, message string) error {
	root, err := RepoRoot(cwd)
	if err != nil {
		return fmt.Errorf("ledgercommit: not a git repo: %w", err)
	}
	return CommitDirtyLedger(root, message)
}

// WriteMergedLedger writes bullseye.yaml under repoRoot after merging with
// HEAD and any working-tree divergence (🎯T1147 path B).
func WriteMergedLedger(repoRoot string, head, working, candidate []byte) ([]byte, error) {
	merged, err := commitscope.UnionPreservingWorkingDivergence(head, working, candidate)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(repoRoot, commitscope.SharedLedger)
	if err := os.WriteFile(path, merged, 0o644); err != nil {
		return nil, err
	}
	return merged, nil
}
