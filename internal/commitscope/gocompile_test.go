// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitscope

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoPackageDirsFromStaged(t *testing.T) {
	got := GoPackageDirsFromStaged([]string{
		"README.md",
		"scripts/journey-suite/chrome_journeys.go",
		"scripts/journey-suite/helper.go",
		"internal/foo/bar.go",
	})
	want := []string{"internal/foo", "scripts/journey-suite"}
	if len(got) != len(want) {
		t.Fatalf("dirs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("dirs = %v, want %v", got, want)
		}
	}
	if GoPackageDirsFromStaged(nil) != nil {
		t.Fatal("nil staged should yield nil dirs")
	}
}

func TestGoCompileRefusalNamesBypass(t *testing.T) {
	msg := GoCompileRefusal("scripts/fix/main.go:3:15: undefined: exec")
	for _, frag := range []string{"🎯T822", "undefined: exec", GoCompileEnv + "=off"} {
		if !strings.Contains(msg, frag) {
			t.Fatalf("message missing %q:\n%s", frag, msg)
		}
	}
}

func TestPackageInModuleFindsNestedGoMod(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/root\n\ngo 1.22\n")
	if err := os.MkdirAll(filepath.Join(dir, "nested", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "nested", "go.mod"), "module example.com/nested\n\ngo 1.22\n")

	mod, pkg := packageInModule(dir, "nested")
	if mod != "nested" || pkg != "./." {
		t.Fatalf("nested root package: mod=%q pkg=%q", mod, pkg)
	}
	mod, pkg = packageInModule(dir, "nested/sub")
	if mod != "nested" || pkg != "./sub" {
		t.Fatalf("nested sub package: mod=%q pkg=%q", mod, pkg)
	}
	mod, pkg = packageInModule(dir, "internal/foo")
	if mod != "." || pkg != "./internal/foo" {
		t.Fatalf("root package: mod=%q pkg=%q", mod, pkg)
	}
}

// TestGoBuildPackagesDropsDeletedDirs pins 🎯T1285: a staged .go path whose
// directory is gone from the extracted index tree is not handed to `go build`.
func TestGoBuildPackagesDropsDeletedDirs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/t1285\n\ngo 1.22\n")
	if err := os.MkdirAll(filepath.Join(dir, "kept"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "kept", "k.go"), "package kept\n")
	line, err := goBuildPackages(dir, []string{"nested", "kept"})
	if err != nil {
		t.Fatalf("deleted nested dir refused: %v\n%s", err, line)
	}
}

func TestGoBuildPackagesCompilesNestedModuleInItsOwnModule(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/root\n\ngo 1.22\n")
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "nested", "go.mod"), "module example.com/nested\n\ngo 1.22\n")
	writeFile(t, filepath.Join(dir, "nested", "main.go"), "package main\nfunc main() {}\n")
	line, err := goBuildPackages(dir, []string{"nested"})
	if err != nil {
		t.Fatalf("nested module should compile inside itself: %v\n%s", err, line)
	}

	writeFile(t, filepath.Join(dir, "nested", "main.go"), "package main\nfunc main() { undefinedX() }\n")
	line, err = goBuildPackages(dir, []string{"nested"})
	if err == nil || !strings.Contains(line, "undefined: undefinedX") {
		t.Fatalf("broken nested module was not refused (err=%v):\n%s", err, line)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
