// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package tmpdirsweep

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestSweepPrefixesRemovesDeadDir(t *testing.T) {
	root := t.TempDir()
	stale := filepath.Join(root, "jevons-journey-dead")
	if err := os.MkdirAll(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * MinStaleAge)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	SweepPrefixes(root, []string{"jevons-journey-"}, nil)
	if _, err := os.Stat(stale); err == nil {
		t.Fatal("expected stale journey dir removed")
	}
}

func TestSweepPrefixesSkipsLiveDir(t *testing.T) {
	if _, err := exec.LookPath("pgrep"); err != nil {
		t.Skip("pgrep unavailable")
	}
	root := t.TempDir()
	live := filepath.Join(root, "jevons-journey-live")
	if err := os.MkdirAll(live, 0o700); err != nil {
		t.Fatal(err)
	}
	sleep := filepath.Join(live, "sleep")
	if err := os.Symlink("/bin/sleep", sleep); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(sleep, "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	SweepPrefixes(root, []string{"jevons-journey-"}, nil)
	if _, err := os.Stat(live); err != nil {
		t.Fatal("live journey dir should not be swept while a process names it")
	}
}

// 🎯T1302: a fresh tree with no process yet is a concurrent suite starting,
// not a dead run.
func TestSweepPrefixesSparesAYoungDir(t *testing.T) {
	root := t.TempDir()
	fresh := filepath.Join(root, "jevons-journey-starting")
	if err := os.MkdirAll(fresh, 0o700); err != nil {
		t.Fatal(err)
	}
	SweepPrefixes(root, []string{"jevons-journey-"}, nil)
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("a just-created journey dir was swept before its suite started a process")
	}
}

// 🎯T1294: only orphans (ppid 1) running an executable from a journey tree
// directly under tmpDir are selected.
func TestOrphanPIDsSelectsOnlyOrphanedJourneyExecutables(t *testing.T) {
	ps := `  101     1 /T/jevons-journey-123/jevonsd -config /T/jevons-journey-123/config.yaml
  102     1 /T/jevons-trust-journey-9/claudia-broker broker serve
  103  4242 /T/jevons-journey-777/jevonsd -config x
  104     1 /Users/me/.bun/bin/bun /T/jevons-journey-123/omp-sidecar/server.ts
  105     1 /T/other-tree/jevonsd
  106     1 /T/jevons-journey-123
  107     1 /elsewhere/T/jevons-journey-1/jevonsd
`
	got := OrphanPIDs(ps, "/T", []string{"jevons-journey-", "jevons-trust-journey-"})
	want := []int{101, 102}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("OrphanPIDs = %v, want %v", got, want)
	}
}
