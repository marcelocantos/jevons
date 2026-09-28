// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 🎯T894: -clean run FROM a sibling repo itself (e.g. verifying a claudia
// commit from inside the claudia checkout) must not inject a replace that
// points claudia's own module at itself — `go` refuses a workspace module
// replaced at all versions, and no tests run at all (specimen: gate c76fcd27
// RED, claudia clean@cfb9024f). The injector must skip the module under
// test while still injecting the other siblings normally.

func writeGoMod(t *testing.T, dir, modulePath string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "module " + modulePath + "\n\ngo 1.26.1\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestOwnModuleName covers the module-name detection the skip decision rests on.
func TestOwnModuleName(t *testing.T) {
	dir := t.TempDir()
	writeGoMod(t, dir, "github.com/marcelocantos/claudia")
	if got := ownModuleName(dir); got != "claudia" {
		t.Fatalf("ownModuleName = %q, want claudia", got)
	}

	empty := t.TempDir()
	if got := ownModuleName(empty); got != "" {
		t.Fatalf("ownModuleName(no go.mod) = %q, want empty", got)
	}
}

// TestInjectCleanSiblingGoWorkSkipsSelf is the claudia-repo case from 🎯T894:
// root IS one of cleanSiblingModules, so injecting that entry must be
// skipped (no self-replacement), while other listed siblings still resolve
// normally if present next to root's parent.
func TestInjectCleanSiblingGoWorkSkipsSelf(t *testing.T) {
	base := t.TempDir() // stands in for the shared-clone parent dir
	root := filepath.Join(base, "claudia")
	writeGoMod(t, root, "github.com/marcelocantos/claudia")

	// vellum sibling present next to root, so it's still eligible.
	vellum := filepath.Join(base, "vellum")
	writeGoMod(t, vellum, "github.com/marcelocantos/vellum")

	wt := filepath.Join(base, "wt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}

	restore := injectCleanSiblingGoWork(root, wt)
	defer restore()

	data, err := os.ReadFile(filepath.Join(wt, "go.work"))
	if err != nil {
		t.Fatalf("go.work not written: %v", err)
	}
	body := string(data)
	if strings.Contains(body, "marcelocantos/claudia =>") {
		t.Fatalf("go.work self-replaces claudia (the module under test):\n%s", body)
	}
	if !strings.Contains(body, "marcelocantos/vellum =>") {
		t.Fatalf("go.work is missing the still-eligible vellum sibling:\n%s", body)
	}
}

// TestInjectCleanSiblingGoWorkJevonsRepo is the non-regression case: root is
// jevons (not itself a listed sibling module), so claudia/vellum siblings
// present next to it are injected exactly as before 🎯T894.
func TestInjectCleanSiblingGoWorkJevonsRepo(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "jevons")
	writeGoMod(t, root, "github.com/marcelocantos/jevons")

	claudia := filepath.Join(base, "claudia")
	writeGoMod(t, claudia, "github.com/marcelocantos/claudia")

	wt := filepath.Join(base, "wt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}

	restore := injectCleanSiblingGoWork(root, wt)
	defer restore()

	data, err := os.ReadFile(filepath.Join(wt, "go.work"))
	if err != nil {
		t.Fatalf("go.work not written: %v", err)
	}
	if !strings.Contains(string(data), "marcelocantos/claudia =>") {
		t.Fatalf("go.work missing claudia sibling injection (regression):\n%s", string(data))
	}
}
