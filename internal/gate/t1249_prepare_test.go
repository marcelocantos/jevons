// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 🎯T1249: Prepare runs after the tree is probed, so files it creates must
// not turn a clean checkout DIRTY; a failing Prepare must not run Command.

func TestT1249PrepareDoesNotDirtyCleanCheckout(t *testing.T) {
	root, _ := fixtureRepo(t, true)
	store := storeAt(t)
	res, err := RunClean(&CleanArgs{
		Command: symbolCheck(), Repo: root, WorkDir: t.TempDir(), Store: store,
		Prepare: func(dir string) error {
			return os.WriteFile(filepath.Join(dir, "generated.txt"), []byte("vendor\n"), 0o644)
		},
		Stdout: nil, Stderr: nil,
	})
	if err != nil {
		t.Fatalf("RunClean: %v", err)
	}
	if res.Record == nil || !res.Record.Verdict.IsGreen() {
		t.Fatalf("want GREEN, got %s", res.Record.Summary())
	}
	if res.Record.Tree == nil || !res.Record.Tree.Clean {
		t.Fatalf("Prepare output must not DIRTY a clean checkout, got %+v", res.Record.Tree)
	}
}

func TestT1249PrepareFailureSkipsCommand(t *testing.T) {
	root, _ := fixtureRepo(t, true)
	store := storeAt(t)
	ran := false
	marker := filepath.Join(t.TempDir(), "ran")
	res, err := RunClean(&CleanArgs{
		Command: []string{"sh", "-c", "echo ran > " + marker},
		Repo:    root, WorkDir: t.TempDir(), Store: store,
		Prepare: func(string) error {
			ran = true
			return os.ErrPermission
		},
		Stdout: nil, Stderr: nil,
	})
	if err == nil {
		t.Fatalf("want Prepare error, got %+v", res)
	}
	if !ran {
		t.Fatal("Prepare was not invoked")
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatal("Command ran after Prepare failed")
	}
	if res != nil && res.Record != nil && res.Record.Verdict.IsGreen() {
		t.Fatalf("failed Prepare attested GREEN: %s", res.Record.Summary())
	}
	if !strings.Contains(err.Error(), "prerequisite") {
		t.Fatalf("error should name prerequisite, got %v", err)
	}
}
