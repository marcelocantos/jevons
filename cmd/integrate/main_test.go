// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main_test

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// Oracle that cmd/integrate is a real CLI entrypoint (worker gate includes
// ./cmd/integrate/... and must execute tests there, not EMPTY).
func TestIntegrateCLIRequiresAgentName(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "integrate")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build integrate: %v\n%s", err, out)
	}
	run := exec.Command(bin)
	run.Dir = t.TempDir()
	err := run.Run()
	exit, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("integrate with no args: %v, want exit error", err)
	}
	if code := exit.ExitCode(); code != 2 {
		t.Fatalf("integrate with no args: exit=%d, want 2", code)
	}
}
