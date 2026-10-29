// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/worktreereap"
)

// 🎯T1165: run from a linked worktree, the development restart hands off to
// the main working tree's script instead of relaunching :13705 on the
// worker's branch. The main tree's copy is a stub here, so the test proves
// the hand-off without starting anything.
func TestT1165RestartFromWorktreeReRootsToSharedClone(t *testing.T) {
	real := readRepo(t, "scripts/restart-jevonsd.sh")
	main := t.TempDir()
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	script := filepath.Join(main, "scripts", "restart-jevonsd.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte(real), 0o755); err != nil {
		t.Fatal(err)
	}
	git(main, "init", "-q")
	git(main, "add", "scripts/restart-jevonsd.sh")
	git(main, "commit", "-q", "-m", "seed")
	wt := filepath.Join(t.TempDir(), "worker")
	git(main, "worktree", "add", "-q", "--detach", wt)
	if err := worktreereap.Mark(&worktreereap.MarkArgs{Worktree: wt, Note: t.Name()}); err != nil {
		t.Fatal(err)
	}

	// The shared clone's copy: a stub that names itself.
	stub := "#!/bin/sh\necho \"MAIN-TREE-SCRIPT root=$(cd \"$(dirname \"$0\")/..\" && pwd) args=$*\"\n"
	if err := os.WriteFile(script, []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}

	bash := "/bin/bash"
	if _, err := os.Stat(bash); err != nil {
		bash = "bash"
	}
	cmd := exec.Command(bash, filepath.Join(wt, "scripts", "restart-jevonsd.sh"), "--dry-run")
	cmd.Env = withoutEnv(os.Environ(), "JEVONS_RESTART_PORT", "JEVONS_RESTART_WORKDIR")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("restart from worktree: %v\n%s", err, out)
	}
	got := string(out)
	mainReal, _ := filepath.EvalSymlinks(main)
	if !strings.Contains(got, "MAIN-TREE-SCRIPT") || !strings.Contains(got, "args=--dry-run") {
		t.Fatalf("worktree restart did not hand off to the main tree's script:\n%s", got)
	}
	if !strings.Contains(got, "root="+mainReal) && !strings.Contains(got, "root="+main) {
		t.Fatalf("hand-off ran from the wrong root (want %s):\n%s", mainReal, got)
	}
	if !strings.Contains(got, "🎯T1165") {
		t.Fatalf("hand-off must say why on stderr:\n%s", got)
	}
}

func withoutEnv(env []string, keys ...string) []string {
	out := env[:0:0]
	for _, kv := range env {
		drop := false
		for _, k := range keys {
			if strings.HasPrefix(kv, k+"=") {
				drop = true
			}
		}
		if !drop {
			out = append(out, kv)
		}
	}
	return out
}
