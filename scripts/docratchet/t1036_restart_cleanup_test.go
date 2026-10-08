// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"fmt"
	"os"
	"os/exec"
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
	cmd := exec.Command(os.Args[0], "-test.run=^TestT1036FailureHelper$", "-test.timeout=3m")
	cmd.Env = append(os.Environ(), "T1036_FAILURE_HELPER=1")
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "intentional fixture failure") {
		t.Fatalf("fixture must fail on purpose: %v\n%s", err, out)
	}
	match := regexp.MustCompile(`T1036_ROOT=(\S+)`).FindStringSubmatch(string(out))
	if len(match) != 2 {
		t.Fatalf("fixture did not identify its temp root:\n%s", out)
	}
	root := match[1]
	deadline := time.Now().Add(10 * time.Second)
	for {
		pids, err := exec.Command("pgrep", "-f", regexp.QuoteMeta(root+"/bin/jevonsd")).Output()
		if err != nil && len(pids) == 0 { // pgrep exits 1 when nothing matches.
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("fixture daemons from %s survived failure: %s", root, pids)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestT1036FailureHelper(t *testing.T) {
	if os.Getenv("T1036_FAILURE_HELPER") == "" {
		t.Skip("subprocess fixture")
	}
	e := newThrashEnv(t)
	e.build("a")
	out, err := e.run(0)
	if err != nil || e.listenerPID() == 0 {
		t.Fatalf("fixture could not start daemon: %v\n%s", err, out)
	}
	fmt.Printf("T1036_ROOT=%s\n", e.root)
	t.Fatal("intentional fixture failure")
}
