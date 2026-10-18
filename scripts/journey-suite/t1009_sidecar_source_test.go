// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia/omp"
)

// 🎯T1009: the journey isolate stages its Claudia sidecar from the pinned
// module. When that broke, the journey suite could not start at all, and
// v0.14.0 and v0.15.0 shipped without a single journey having run: the
// release path runs go test ./..., not the journeys, which need live
// providers. This test runs in that go test, so a pin or a stand-in that
// takes the sidecar source away fails the release build instead.
func TestJourneyIsolateFindsTheSidecarInThePinnedModule(t *testing.T) {
	t.Setenv("CLAUDIA_OMP_SERVER", "")
	script := omp.ServerScript()
	if strings.TrimSpace(script) == "" {
		t.Fatal("the pinned claudia module names no sidecar server script")
	}
	dir := filepath.Dir(script)
	for _, name := range []string{"server.ts", "package.json", "bun.lockb"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("journey isolate cannot stage the sidecar: %s missing from %s: %v", name, dir, err)
		}
	}
	manifest, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(manifest), "patches/") {
		patches, err := os.ReadDir(filepath.Join(dir, "patches"))
		if err != nil || len(patches) == 0 {
			t.Fatalf("package.json names patches/ but the pinned sidecar has none: %v", err)
		}
	}
}

func TestIsolatedSidecarStagesPatchFiles(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "patches"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "node_modules", "secret"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(rel, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(src, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", `{"patchedDependencies":{"x":"patches/x.patch"}}`)
	write("bun.lock", "lock")
	write("server.ts", "export {}\n")
	write("patches/x.patch", "diff\n")
	write("node_modules/secret/index.js", "nope\n")
	if err := copyIsolatedSidecarSource(src, dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "patches", "x.patch"))
	if err != nil || string(got) != "diff\n" {
		t.Fatalf("patch = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dst, "bun.lock")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "node_modules")); !os.IsNotExist(err) {
		t.Fatalf("node_modules staged: %v", err)
	}
}
