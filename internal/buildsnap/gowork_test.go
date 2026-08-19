// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package buildsnap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInjectSiblingGoWorkWritesAndRestores(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "jevons")
	sib := filepath.Join(base, "claudia")
	snap := filepath.Join(t.TempDir(), "snap")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(snap, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sib, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sib, "go.mod"), []byte("module github.com/marcelocantos/claudia\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore, err := injectSiblingGoWork(Config{RepoRoot: root, SnapDir: snap})
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(snap, "go.work"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), sib) {
		t.Fatalf("go.work missing sibling path: %s", body)
	}
	restore()
	if _, err := os.Stat(filepath.Join(snap, "go.work")); !os.IsNotExist(err) {
		t.Fatalf("go.work should be removed on restore, err=%v", err)
	}
}

func TestInjectSiblingGoWorkNoopWithoutSibling(t *testing.T) {
	root := filepath.Join(t.TempDir(), "jevons")
	snap := filepath.Join(t.TempDir(), "snap")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(snap, 0o755); err != nil {
		t.Fatal(err)
	}
	restore, err := injectSiblingGoWork(Config{RepoRoot: root, SnapDir: snap})
	if err != nil {
		t.Fatal(err)
	}
	restore()
	if _, err := os.Stat(filepath.Join(snap, "go.work")); !os.IsNotExist(err) {
		t.Fatal("wrote go.work without a sibling claudia")
	}
}
