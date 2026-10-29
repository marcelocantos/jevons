// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// 🎯T831: J31's packaged-React owner/PRE prompts must stay short and
// single-line so the real provider does not treat them as collapsed paste.
func TestT831J31BoundaryPromptHermetic(t *testing.T) {
	root, err := j19RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "scripts", "react-ui-test", "boundary-prompt.test.cjs")
	cmd := exec.Command("node", "--test", script)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node --test boundary-prompt.test.cjs: %v\n%s", err, out)
	}
	if len(out) == 0 {
		t.Fatal("expected test output")
	}
	t.Log(string(out))
}
