// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package treeguard

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 🎯T858: pre-commit runs `treeguard sweep`, which must create temps under
// journal/. A codex seat whose writable roots omit ~/.jevons/treeguard hits
// "operation not permitted" and the failed-open line (🎯T391).
func TestSweepJournalWriteNeedsWritableJournalDir(t *testing.T) {
	root := t.TempDir()
	t.Setenv(StoreDirEnv, root)
	store := &Store{Root: root}

	journal := filepath.Join(root, journalDir)
	if err := os.MkdirAll(journal, 0o555); err != nil {
		t.Fatal(err)
	}
	err := store.Journal(filepath.Join(t.TempDir(), "guarded.txt"), []byte("x"), time.Now(), "", ViaSweep)
	if err == nil {
		t.Fatal("expected journal write to fail when journal dir is not writable")
	}

	if err := os.Chmod(journal, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := store.Journal(filepath.Join(t.TempDir(), "guarded.txt"), []byte("x"), time.Now(), "", ViaSweep); err != nil {
		t.Fatalf("journal write should succeed when journal dir is writable: %v", err)
	}
}
