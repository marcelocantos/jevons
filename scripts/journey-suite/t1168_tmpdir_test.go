// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/tmpdirsweep"
)

// countJourneyDirs counts immediate children of dir whose names match the
// suite's isolate prefixes.
func countJourneyDirs(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		for _, p := range journeyTempPrefixes {
			if strings.HasPrefix(name, p) {
				n++
				break
			}
		}
	}
	return n
}

// 🎯T1168 hermetic: the same teardown path that registers an isolate state
// dir must remove it on abnormal exit, leaving no jevons-journey-* behind.
func TestT1168IsolateStateDirRemovedOnFatal(t *testing.T) {
	tmp := t.TempDir()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(),
		childEnv+"=state-dir-fatal",
		"TMPDIR="+tmp,
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("child should fail: %v\n%s", err, out)
	}
	if countJourneyDirs(tmp) != 0 {
		t.Fatalf("jevons-journey-* left in TMPDIR after fatal exit:\n%s", listJourneyDirs(tmp))
	}
}

func TestT1168SweepRemovesDeadIsolateDir(t *testing.T) {
	tmp := t.TempDir()
	stale := filepath.Join(tmp, "jevons-journey-stale")
	if err := os.MkdirAll(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	// A dead run's tree is old; a fresh one is a suite starting (🎯T1302).
	old := time.Now().Add(-2 * tmpdirsweep.MinStaleAge)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	tmpdirsweep.SweepPrefixes(tmp, journeyTempPrefixes, killPrivateClaudiaTmux)
	if _, err := os.Stat(stale); err == nil {
		t.Fatal("sweep should remove dead isolate dir")
	}
}

func listJourneyDirs(dir string) string {
	var names []string
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return strings.Join(names, "\n")
}
