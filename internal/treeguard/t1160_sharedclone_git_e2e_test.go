// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package treeguard

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/commitattrib"
)

// 🎯T1160: a fleet work seat still in the shared clone must not hand-commit or
// merge onto the integration branch (integrate-refusal fallback shape).
func TestT1160FleetSeatInSharedCloneRefusesGitCommitAndMerge(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	base := filepath.Join(dir, "jevons")
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", base}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	run("init", "-q", "-b", "master")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(base, "seed.txt"), []byte("s\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "seed")
	run("branch", "feature/hand-land")
	t.Setenv(commitattrib.ActorEnv, "jv-t958-auto")
	env := &Env{Store: &Store{Root: t.TempDir()}, RepoRoot: base, Now: time.Now}
	for _, cmd := range []string{"git commit -m must-not-land", "git merge --ff-only feature/hand-land"} {
		got, err := env.Pre(bashPayload(base, cmd))
		if err != nil {
			t.Fatalf("Pre(%q): %v", cmd, err)
		}
		if got.Verdict != Deny {
			t.Fatalf("Pre(%q) = %+v, want Deny", cmd, got)
		}
		if !strings.Contains(got.Message, "jv-t958-auto") || !strings.Contains(got.Message, "T1160") {
			t.Fatalf("refusal must name seat and T1160: %q", got.Message)
		}
	}
}
