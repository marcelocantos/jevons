// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package buildsnap

import (
	"fmt"
	"os"
	"path/filepath"
)

// injectSiblingGoWork writes a snapshot go.work that replaces claudia
// with the org-sibling checkout, when one exists next to the clone.
//
// Local claudia upgrades ahead of the published pin are not always
// in go.mod.
// ../go.work hides that in the shared clone; the 🎯T254.2 snapshot is a
// worktree of jevons HEAD alone and GOWORK=off's pin cannot see them.
// A snapshot-only go.work with an absolute replace is the T473 shape
// for an unpublished sibling: committed jevons stays pin-clean (T448).
func injectSiblingGoWork(cfg Config) (restore func(), err error) {
	noop := func() {}
	sibling := filepath.Join(filepath.Dir(cfg.RepoRoot), "claudia")
	if _, err := os.Stat(filepath.Join(sibling, "go.mod")); err != nil {
		return noop, nil
	}
	path := filepath.Join(cfg.SnapDir, "go.work")
	if _, err := os.Stat(path); err == nil {
		// Committed workspace in the snapshot — leave it.
		return noop, nil
	}
	body := fmt.Sprintf("go 1.26.1\n\nuse .\n\nreplace github.com/marcelocantos/claudia => %s\n", sibling)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return noop, fmt.Errorf("write snapshot go.work: %w", err)
	}
	cfg.logf("snapshot go.work: claudia => %s (unpublished sibling)", sibling)
	return func() {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			cfg.logf("WARNING: could not remove snapshot go.work (%v)", err)
		}
	}, nil
}
