// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestEnsureJourneyGitRepo(t *testing.T) {
	dir := t.TempDir()
	if err := ensureJourneyGitRepo(dir); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "rev-parse", "--path-format=absolute", "--git-dir")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), ".git") {
		t.Fatalf("git dir = %q", out)
	}
}
