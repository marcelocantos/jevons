// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/gate"
)

// Exercise the outer CLI -> make -> inner CLI -> make chain with one store.
// Before T997 the inner gate waits for the outer's flock forever. The bounded
// process-group cleanup makes that failure reproducible without orphan gates.
func TestT997NestedHeavyGate(t *testing.T) {
	bin := gateBinary(t)
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[fail], func(t *testing.T) {
			dir := t.TempDir()
			leaf := "echo nested-leaf"
			if fail {
				leaf = "exit 7"
			}
			mk := "test-outer:\n\t\"" + bin + "\" -- make test-inner\ntest-inner:\n\t" + leaf + "\n"
			if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(mk), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(bin, "--", "make", "test-outer")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), gate.StoreDirEnv+"="+filepath.Join(dir, "store"))
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			var output strings.Builder
			cmd.Stdout, cmd.Stderr = &output, &output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case err := <-done:
				if (err != nil) != fail {
					t.Fatalf("err=%v output=%s", err, output.String())
				}
				if strings.Count(output.String(), "GATE ") != 2 {
					t.Fatalf("missing nested records: %s", output.String())
				}
			case <-time.After(8 * time.Second):
				ps, _ := exec.Command("ps", "-axo", "pid=,ppid=,command=").Output()
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				<-done
				for _, line := range strings.Split(string(ps), "\n") {
					if strings.Contains(line, bin) || strings.Contains(line, "make test-") {
						t.Log(line)
					}
				}
				t.Fatalf("nested heavy gates deadlocked; outer pid=%d output=%s", cmd.Process.Pid, output.String())
			}
		})
	}
}
