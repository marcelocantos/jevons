// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitscope_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStaleBundleGuardThroughTheShippedHook drives the real pre-commit shim in
// a scratch repo: refuse ui/src without the bundle, allow it with the bundle,
// leave a non-ui commit alone, and judge the COMMITTED tree, not the work tree,
// when another worker holds dirty ui/src edits (🎯T377/T398).
func TestStaleBundleGuardThroughTheShippedHook(t *testing.T) {
	r := newGuardedRepo(t)
	if err := os.MkdirAll(filepath.Join(r.dir, "ui", "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.write(t, "ui/src/App.tsx", "v1")
	r.write(t, "ui/bundle.zip", "b1")
	r.write(t, "ui/src/App.test.tsx", "t1")
	r.write(t, "go.txt", "g1")
	r.git(t, "add", ".")
	if out, err := r.git(t, "commit", "-q", "-m", "seed", "--no-verify"); err != nil {
		t.Fatalf("seed: %v\n%s", err, out)
	}

	// Another worker's uncommitted ui/src edit sits in the work tree.
	r.write(t, "ui/src/Other.tsx", "foreign wip")

	// Refused: source, no bundle. Message names the asset.
	r.write(t, "ui/src/App.tsx", "v2")
	before := r.head(t)
	out, err := r.git(t, "commit", "--only", "ui/src/App.tsx", "-m", "src only")
	if err == nil || !strings.Contains(out, "ui/src/App.tsx") || !strings.Contains(out, "ui/bundle.zip") {
		t.Fatalf("stale-bundle commit was not refused with names (err=%v):\n%s", err, out)
	}
	if r.head(t) != before {
		t.Fatal("HEAD moved on a refused commit")
	}

	// Allowed: same change with a rebuilt bundle; foreign wip stays out.
	r.write(t, "ui/bundle.zip", "b2")
	if out, err := r.git(t, "commit", "--only", "ui/src/App.tsx", "ui/bundle.zip", "-m", "src+bundle"); err != nil {
		t.Fatalf("src+bundle refused:\n%s", out)
	}
	if strings.Contains(r.showStat(t, "HEAD"), "Other.tsx") {
		t.Fatal("foreign work tree edit leaked into the commit")
	}

	// Judged on the committed tree: a commit touching only go.txt is not
	// refused even though the work tree has dirty ui/src.
	r.write(t, "go.txt", "g2")
	if out, err := r.git(t, "commit", "--only", "go.txt", "-m", "non-ui"); err != nil {
		t.Fatalf("non-ui commit refused by dirty foreign ui/src:\n%s", out)
	}

	// Test-only ui change needs no bundle.
	r.write(t, "ui/src/App.test.tsx", "t2")
	if out, err := r.git(t, "commit", "--only", "ui/src/App.test.tsx", "-m", "test only"); err != nil {
		t.Fatalf("test-only ui commit refused:\n%s", out)
	}

	// Bypass works and is logged.
	r.write(t, "ui/src/App.tsx", "v3")
	if out, err := r.gitEnv(t, []string{"JEVONS_UI_BUNDLE=off"}, "commit", "--only", "ui/src/App.tsx", "-m", "bypass"); err != nil {
		t.Fatalf("bypass refused:\n%s", out)
	}
	log, err := os.ReadFile(filepath.Join(r.dir, ".git", "ui-bundle-bypass.log"))
	if err != nil || !strings.Contains(string(log), "ui/src/App.tsx") {
		t.Fatalf("bypass not audited: %v %q", err, log)
	}
}
