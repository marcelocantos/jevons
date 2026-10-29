// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate_test

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

// The launcher exits before the command does. Its handle must lead to the
// command's actual stored verdict, not caller_gone or a guessed zero status.
func TestT740BackgroundGateReadBack(t *testing.T) {
	bin := gateBinary(t)
	for _, tc := range []struct {
		name, command, verdict string
		status                 int
	}{
		{"passing", "sleep 0.3; echo completed", "GREEN", 0},
		{"failing", "sleep 0.3; echo failed; exit 7", "RED", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := t.TempDir()
			out, err := exec.Command(bin, "-store", store, "-background", "--", "sh", "-c", tc.command).CombinedOutput()
			if err != nil {
				t.Fatalf("launch: %v: %s", err, out)
			}
			line := string(out)
			start := strings.Index(line, "handle=")
			if start < 0 {
				t.Fatalf("no handle: %s", line)
			}
			handle := strings.TrimSuffix(strings.Fields(line[start+len("handle="):])[0], ";")
			deadline := time.Now().Add(10 * time.Second)
			for time.Now().Before(deadline) {
				result, err := exec.Command(bin, "-store", store, "wait", handle).CombinedOutput()
				if strings.Contains(string(result), "pending") {
					time.Sleep(30 * time.Millisecond)
					continue
				}
				if !strings.Contains(string(result), "exit=") || !strings.Contains(string(result), tc.verdict) {
					t.Fatalf("wait: %v: %s", err, result)
				}
				if (err == nil) != (tc.status == 0) {
					t.Fatalf("wrong read-back status %v: %s", err, result)
				}
				return
			}
			t.Fatalf("gate never finished: %s", handle)
		})
	}
}
