// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package turnrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 🎯T392.1: no spend path remints, rewinds, or injects a T285 seed.
func TestSpendPathSourceDoesNotRemintRewindOrSeed(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	banned := []string{
		"PrepareCompaction",
		"CompactOverseer",
		"SeedSuccessor",
		"compactOrRotate",
		".rotate(",
		"Rewind",
		"KindCompact",
		"KindMigrate",
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(".", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		src := string(body)
		for _, bad := range banned {
			if strings.Contains(src, bad) {
				t.Errorf("%s still names %s — spend path must not remint/rewind/seed", e.Name(), bad)
			}
		}
	}
}
