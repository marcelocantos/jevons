// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestT710MakefileCleanRemovesBin: make clean is what accounts for
// unbuildable strays in gitignored bin/ (the Apr 2026 bin/jevond incident).
func TestT710MakefileCleanRemovesBin(t *testing.T) {
	mk := readRepo(t, "Makefile")
	if !strings.Contains(mk, ".PHONY: clean") {
		t.Fatal("Makefile missing .PHONY: clean")
	}
	if !strings.Contains(mk, "rm -rf bin") {
		t.Fatal("make clean must remove bin/ (🎯T710 unbuildable strays)")
	}
	if !strings.Contains(mk, "🎯T710") {
		t.Fatal("make clean must name 🎯T710 so the stray recipe stays load-bearing")
	}
}

// TestT710NoJevondProduct: there is no cmd/jevond and no Makefile rule that
// would rebuild the stale binary a seat launches by mistyping jevonsd.
func TestT710NoJevondProduct(t *testing.T) {
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "cmd", "jevond")); !os.IsNotExist(err) {
		t.Fatalf("cmd/jevond must not exist: %v", err)
	}
	mk := readRepo(t, "Makefile")
	for _, line := range strings.Split(mk, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "bin/jevond:") || trim == "jevond:" || strings.HasPrefix(trim, "jevond:") {
			t.Fatalf("Makefile must not build jevond: %q", line)
		}
	}
}

func TestT710DaemonAndWatchdogWireDetection(t *testing.T) {
	daemon := readRepo(t, "cmd/jevonsd/main.go")
	if !strings.Contains(daemon, "portown.WatchLoop") {
		t.Fatal("jevonsd must run portown.WatchLoop after bind")
	}
	wd := readRepo(t, "cmd/jevons-watchdog/main.go")
	if !strings.Contains(wd, "inspectPortOwnership") {
		t.Fatal("watchdog must inspect port ownership each cycle")
	}
	if !strings.Contains(wd, "independent of Decide") {
		t.Fatal("watchdog must not fold a squatter into ActionRestart")
	}
}
