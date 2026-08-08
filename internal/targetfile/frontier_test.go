// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package targetfile

import (
	"os"
	"path/filepath"
	"testing"
)

const frontierTestLedger = `
targets:
  T1:
    name: Root achieved
    status: achieved
  T2:
    name: Ready leaf
    status: identified
    acceptance:
    - does the thing
    tags:
    - product
  T3:
    name: Blocked leaf
    status: identified
    depends_on:
    - T2
  T4:
    name: Ready after done dep
    status: converging
    depends_on:
    - T1
  T5:
    name: Unknown dep blocks
    status: identified
    depends_on:
    - T999
  T10:
    name: Natural order after T4
    status: identified
`

// 🎯T254.1: frontier = active targets with all deps done; natural id order.
func TestFrontierLeaves(t *testing.T) {
	leaves, err := FrontierLeaves([]byte(frontierTestLedger))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, l := range leaves {
		ids = append(ids, l.ID)
	}
	want := []string{"T2", "T4", "T10"}
	if len(ids) != len(want) {
		t.Fatalf("leaves = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("leaves = %v, want %v", ids, want)
		}
	}
	if leaves[0].Name != "Ready leaf" || len(leaves[0].Acceptance) != 1 || len(leaves[0].Tags) != 1 {
		t.Fatalf("leaf fields not carried: %+v", leaves[0])
	}
}

func TestLoadFrontierLeavesFromCwd(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte(frontierTestLedger), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "internal", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	leaves, ledger, err := LoadFrontierLeavesFromCwd(sub)
	if err != nil {
		t.Fatal(err)
	}
	if ledger != filepath.Join(dir, "bullseye.yaml") {
		t.Fatalf("ledger path = %q", ledger)
	}
	if len(leaves) != 3 {
		t.Fatalf("got %d leaves, want 3", len(leaves))
	}
}

func TestTargetIDNaturalLess(t *testing.T) {
	ordered := []string{"T1", "T1.1", "T2", "T10", "T10.2", "T10.10", "T27", "T100"}
	for i := 0; i < len(ordered)-1; i++ {
		if !targetIDNaturalLess(ordered[i], ordered[i+1]) {
			t.Errorf("want %s < %s", ordered[i], ordered[i+1])
		}
		if targetIDNaturalLess(ordered[i+1], ordered[i]) {
			t.Errorf("want NOT %s < %s", ordered[i+1], ordered[i])
		}
	}
}
