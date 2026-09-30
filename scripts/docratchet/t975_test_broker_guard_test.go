// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestT975EveryRegistryTestPackageIsGuarded ratchets 🎯T975: a Go package
// whose tests build a claudia registry cannot reach the owner's development
// broker. On 2026-10-01 an unguarded test minted a real seat on xAI Grok on
// every run. The guard is testbroker.DeadEnd (or, in mcpserver, the
// equivalent TestMain). The journey suite runs isolated brokers by design.
func TestT975EveryRegistryTestPackageIsGuarded(t *testing.T) {
	root := repoRoot(t)
	exempt := map[string]bool{"scripts/journey-suite": true}
	pkgs := map[string]bool{}
	guarded := map[string]bool{}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", ".claude", "_scratchpad":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, filepath.Dir(path))
		body := string(raw)
		if strings.Contains(body, "NewRegistry(") {
			pkgs[rel] = true
		}
		if strings.Contains(body, "testbroker.DeadEnd()") || strings.Contains(body, `os.Setenv("CLAUDIA_BROKER_SOCKET"`) {
			guarded[rel] = true
		}
		return nil
	})
	if len(pkgs) == 0 {
		t.Fatal("found no test package building a registry; the scan is broken")
	}
	for p := range pkgs {
		if !exempt[p] && !guarded[p] {
			t.Errorf("%s: tests build a claudia registry but nothing keeps them off the owner's broker; add testbroker_guard_test.go (🎯T975)", p)
		}
	}
}
