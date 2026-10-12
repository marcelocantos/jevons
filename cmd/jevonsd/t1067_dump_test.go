// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 🎯T1067: a diagnostic dump writes every goroutine's stack and the heap
// profile, and the daemon keeps running.
func TestT1067WriteDumpHasGoroutinesAndHeap(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dumps")
	at := time.Date(2026, 10, 12, 7, 0, 0, 0, time.UTC)
	if _, err := writeDump(dir, at); err != nil {
		t.Fatal(err)
	}
	g, err := os.ReadFile(filepath.Join(dir, "20261012T070000Z-goroutine.txt"))
	if err != nil || !strings.Contains(string(g), "TestT1067WriteDumpHasGoroutinesAndHeap") {
		t.Fatalf("goroutine dump lacks this test's own stack (err=%v)", err)
	}
	for _, name := range []string{"20261012T070000Z-heap.pprof", "20261012T070000Z-memstats.txt"} {
		if st, err := os.Stat(filepath.Join(dir, name)); err != nil || st.Size() == 0 {
			t.Fatalf("%s missing or empty (err=%v)", name, err)
		}
	}
}
