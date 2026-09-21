// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package upgrade

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// listStoreWriterPIDs keeps 🎯T541.1's tests on the signature they were
// written against.
func listStoreWriterPIDs(path string) []int {
	pids, _ := storeWriterPIDs(path)
	return pids
}

// 🎯T766: an lsof that never answers held jevonsd's boot for as long as it
// hung, so nothing after ReattachFleetContext — the cockpit loop, the fleet
// pass — ever started. The probe is bounded, and not knowing fails closed.
func TestStoreHolderProbeIsBoundedAndFailsClosed(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "store.db")
	if err := os.WriteFile(store, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	hang := filepath.Join(dir, "lsof")
	if err := os.WriteFile(hang, []byte("#!/bin/sh\nexec sleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldCmd, oldTimeout := lsofCommand, lsofTimeout
	lsofCommand, lsofTimeout = hang, 200*time.Millisecond
	defer func() { lsofCommand, lsofTimeout = oldCmd, oldTimeout }()

	start := time.Now()
	pids, ok := storeWriterPIDs(store)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("holder probe blocked for %s; it must be bounded", elapsed)
	}
	if ok || len(pids) != 0 {
		t.Fatalf("a probe that timed out reported an answer: pids=%v ok=%v", pids, ok)
	}
}
