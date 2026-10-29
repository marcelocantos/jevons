// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 🎯T780 — shell load loops must register trap cleanup so SIGTERM from a
// gate timeout still kills spinners (squz/ge stress incident).
func TestT780ShellLoadLoopsUseTrapCleanup(t *testing.T) {
	root := repoRoot(t)
	scripts := filepath.Join(root, "scripts")
	entries, err := os.ReadDir(scripts)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sh") {
			continue
		}
		path := filepath.Join(scripts, e.Name())
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		if !strings.Contains(text, "while :") {
			continue
		}
		if strings.Contains(text, "trap ") {
			continue
		}
		t.Errorf("%s: contains a while-loop load generator but no trap handler (🎯T780)", path)
	}
}
