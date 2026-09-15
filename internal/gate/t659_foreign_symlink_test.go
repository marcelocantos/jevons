// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"os"
	"path/filepath"
	"testing"
)

// TestT659UnlinkForeignSymlinksKeepsTargets is the unit half of 🎯T659: a
// symlink from a throwaway tree into somewhere else is unlinked, not
// followed; a symlink that stays inside the tree is left alone; and the
// target directory keeps every byte it had.
func TestT659UnlinkForeignSymlinksKeepsTargets(t *testing.T) {
	scratch := t.TempDir()
	shared := filepath.Join(scratch, "shared", "node_modules")
	for _, pkg := range []string{"vitest", "vite"} {
		if err := os.MkdirAll(filepath.Join(shared, pkg), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(shared, pkg, "index.js"), []byte(pkg), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	wt := filepath.Join(scratch, "wt")
	if err := os.MkdirAll(filepath.Join(wt, "ui", "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(wt, ".git", "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(wt, "ui", "node_modules")
	if err := os.Symlink(shared, foreign); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(wt, "ui", "src-link")
	if err := os.Symlink(filepath.Join(wt, "ui", "src"), local); err != nil {
		t.Fatal(err)
	}
	dangling := filepath.Join(wt, "ui", "gone")
	if err := os.Symlink(filepath.Join(scratch, "nowhere"), dangling); err != nil {
		t.Fatal(err)
	}

	unlinked, err := UnlinkForeignSymlinks(wt)
	if err != nil {
		t.Fatalf("UnlinkForeignSymlinks: %v", err)
	}
	if len(unlinked) != 2 {
		t.Fatalf("unlinked %v, want the foreign link and the dangling link only", unlinked)
	}
	if _, err := os.Lstat(foreign); !os.IsNotExist(err) {
		t.Fatalf("foreign symlink still present: %v", err)
	}
	if _, err := os.Lstat(dangling); !os.IsNotExist(err) {
		t.Fatalf("dangling symlink still present: %v", err)
	}
	if _, err := os.Lstat(local); err != nil {
		t.Fatalf("in-tree symlink was removed: %v", err)
	}
	for _, pkg := range []string{"vitest", "vite"} {
		b, err := os.ReadFile(filepath.Join(shared, pkg, "index.js"))
		if err != nil || string(b) != pkg {
			t.Fatalf("shared %s lost its contents: %v %q", pkg, err, b)
		}
	}
}
