// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Exercise the real restart script in a second test binary. A deliberately
// failed test cannot be asserted from inside itself after its cleanups run.
func TestT1036FailedRestartFixtureLeavesNoDaemon(t *testing.T) {
	if _, err := os.Stat("/usr/sbin/lsof"); err != nil {
		t.Skip("lsof unavailable")
	}
	for _, tc := range []struct {
		name, mode, timeout, failure string
	}{
		{"failure", "fail", "3m", "intentional fixture failure"},
		{"timeout", "timeout", "3m", "test timed out"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The helper compiles four Go binaries and starts the restart script
			// before it writes the readiness marker. Under whole-module load that
			// startup took over 30s, so a 30s Go test timeout killed setup
			// before any daemon existed. Budget the compile/startup separately
			// from the intentional timeout, and require the marker below.
			cmd := exec.Command(os.Args[0], "-test.run=^TestT1036FailureHelper$", "-test.timeout="+tc.timeout)
			marker := filepath.Join(t.TempDir(), "fixture-root")
			cmd.Env = append(os.Environ(), "T1036_FAILURE_HELPER="+tc.mode, "T1036_ROOT_MARKER="+marker)
			out, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(out), tc.failure) {
				t.Fatalf("fixture must end by %s: %v\n%s", tc.name, err, out)
			}
			rootBytes, readErr := os.ReadFile(marker)
			if readErr != nil {
				t.Fatalf("fixture did not identify its temp root: %v\n%s", readErr, out)
			}
			root := string(rootBytes)
			deadline := time.Now().Add(10 * time.Second)
			for {
				pids, err := exec.Command("pgrep", "-f", regexp.QuoteMeta(root+"/bin/jevonsd")).Output()
				if err != nil && len(pids) == 0 { // pgrep exits 1 when nothing matches.
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("fixture daemons from %s survived %s: %s", root, tc.name, pids)
				}
				time.Sleep(100 * time.Millisecond)
			}
		})
	}
}

func TestT1036FailureHelper(t *testing.T) {
	mode := os.Getenv("T1036_FAILURE_HELPER")
	if mode == "" {
		t.Skip("subprocess fixture")
	}
	e := newThrashEnv(t)
	e.build("a")
	out, err := e.run()
	if err != nil || e.listenerPID() == 0 {
		t.Fatalf("fixture could not start daemon: %v\n%s", err, out)
	}
	if err := os.WriteFile(os.Getenv("T1036_ROOT_MARKER"), []byte(e.root), 0o600); err != nil {
		t.Fatal(err)
	}
	if mode == "timeout" {
		select {} // testing timeout exits without running t.Cleanup.
	}
	t.Fatal("intentional fixture failure")
}
