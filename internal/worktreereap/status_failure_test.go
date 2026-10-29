// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktreereap

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestT107413SweepHoldsUnknownStatusAndRemovesVerifiedCleanTree(t *testing.T) {
	root := tempRepo(t)
	dirty := addWorktree(t, root, "dirty")
	clean := addWorktree(t, root, "clean")
	const contents = "uncommitted source that exists only in this checkout\n"
	if err := os.WriteFile(filepath.Join(dirty, "file.txt"), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	// Fail only the real observation under test; list, removal and the clean
	// fixture's status still use real Git. This never exhausts host resources.
	wrapper := "#!/bin/sh\n" +
		"if [ \"$1\" = status ] && [ \"$PWD\" = \"$JEVONS_TEST_STATUS_FAIL_DIR\" ]; then\n" +
		"  echo 'resource temporarily unavailable: simulated git status spawn failure' >&2\n" +
		"  exit 1\nfi\nexec '" + strings.ReplaceAll(realGit, "'", "'\\''") + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(wrapper), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("JEVONS_TEST_STATUS_FAIL_DIR", dirty)
	res, err := Sweep(aggressive(root))
	if err != nil {
		t.Fatal(err)
	}
	var held bool
	for _, verdict := range res.Verdicts {
		if verdict.Entry.Path == dirty {
			held = verdict.Decision.Action == ActionHold && !verdict.Removed &&
				strings.Contains(verdict.Decision.Reason, "cannot establish tracked-file status") &&
				strings.Contains(verdict.Decision.Reason, "resource temporarily unavailable")
		}
	}
	if !held || !listed(t, root, dirty) {
		t.Fatalf("dirty checkout was not held with an actionable observation failure:\n%s", res.Report())
	}
	data, err := os.ReadFile(filepath.Join(dirty, "file.txt"))
	if err != nil || string(data) != contents {
		t.Fatalf("uncommitted source changed: %q, %v", data, err)
	}
	if listed(t, root, clean) {
		t.Fatalf("verified clean abandoned checkout was not removed:\n%s", res.Report())
	}
	if _, err := os.Stat(clean); !os.IsNotExist(err) {
		t.Fatalf("clean checkout directory remains: %v", err)
	}
}
