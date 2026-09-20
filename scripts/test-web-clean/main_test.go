// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestManifestSeesEveryKindOfChange pins the before/after comparison the
// gate's byte-identical claim rests on: a touched file, a removed package
// and a retargeted symlink all show up, and an unchanged tree diffs empty.
func TestManifestSeesEveryKindOfChange(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "node_modules")
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("vitest/index.js", "v1")
	write("vite/index.js", "vite")
	if err := os.MkdirAll(filepath.Join(dir, ".bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../vitest/index.js", filepath.Join(dir, ".bin", "vitest")); err != nil {
		t.Fatal(err)
	}

	before, err := manifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	same, err := manifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d := diff(before, same); len(d) != 0 {
		t.Fatalf("unchanged tree diffs as %v", d)
	}

	write("vitest/index.js", "v2")
	if err := os.RemoveAll(filepath.Join(dir, "vite")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, ".bin", "vitest")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../vite/index.js", filepath.Join(dir, ".bin", "vitest")); err != nil {
		t.Fatal(err)
	}
	after, err := manifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := diff(before, after)
	want := []string{
		"changed .bin/vitest",
		"changed vitest/index.js",
		"removed vite",
		"removed vite/index.js",
	}
	if len(got) != len(want) {
		t.Fatalf("diff = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("diff = %v, want %v", got, want)
		}
	}

	// The incident shape: node_modules emptied entirely.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	empty, err := manifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 || len(diff(before, empty)) == 0 {
		t.Fatalf("an emptied node_modules must diff against the original; got %v", diff(before, empty))
	}
}
