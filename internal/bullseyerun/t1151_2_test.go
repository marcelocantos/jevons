// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package bullseyerun_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/marcelocantos/jevons/internal/bullseyerun"
	"github.com/marcelocantos/jevons/internal/targetfile"
	"github.com/marcelocantos/jevons/internal/worktreereap"
)

func requireCommitModeBullseye(t *testing.T) {
	t.Helper()
	if os.Getenv("BULLSEYE_BIN") == "" {
		if home, err := os.UserHomeDir(); err == nil {
			cargo := filepath.Join(home, ".cargo", "bin", "bullseye")
			if _, err := os.Stat(cargo); err == nil {
				t.Setenv("BULLSEYE_BIN", cargo)
			}
		}
	}
	dir := t.TempDir()
	initGitRepo(t, dir)
	out, err := bullseyerun.RunMutation(
		"commit", "--op", "track", "--cwd", dir,
		"--name", "commit-mode probe", "--acceptance", "ok",
	)
	if err != nil {
		t.Skipf("bullseye commit mode unavailable: %v\n%s", err, out)
	}
	log := gitOut(t, dir, "log", "--oneline", "-2")
	if !strings.Contains(log, "bullseye: ledger mutation") {
		t.Skipf("installed bullseye did not create a ledger commit (log=%q)", log)
	}
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	git(t, dir, "init")
	git(t, dir, "config", "user.email", "t@test")
	git(t, dir, "config", "user.name", "t")
	writeFile(t, filepath.Join(dir, "bullseye.yaml"), "targets: {}\n")
	git(t, dir, "add", "bullseye.yaml")
	git(t, dir, "commit", "-m", "init")
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", "-C", dir)
	cmd.Args = append(cmd.Args, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", dir)
	cmd.Args = append(cmd.Args, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return string(out)
}


func samePath(a, b string) bool {
	a, _ = filepath.EvalSymlinks(filepath.Clean(a))
	b, _ = filepath.EvalSymlinks(filepath.Clean(b))
	return a == b
}

func ledgerClean(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "diff", "--quiet", "HEAD", "--", "bullseye.yaml")
	if err := cmd.Run(); err != nil {
		out := gitOut(t, dir, "status", "--porcelain", "--", "bullseye.yaml")
		t.Fatalf("bullseye.yaml not clean vs HEAD: %q", out)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWriteRootsWorktreeMapsToMain(t *testing.T) {
	clone := t.TempDir()
	initGitRepo(t, clone)
	wt := filepath.Join(clone, "wt")
	git(t, clone, "worktree", "add", wt)
	if err := worktreereap.Mark(&worktreereap.MarkArgs{Worktree: wt, Note: t.Name()}); err != nil {
		t.Fatal(err)
	}

	commit, mirror := targetfile.WriteRoots(wt)
	if !samePath(commit, clone) || !samePath(mirror, wt) {
		t.Fatalf("WriteRoots(%q) = commit %q mirror %q; want %q / %q", wt, commit, mirror, clone, wt)
	}
}

func TestRunMutationFromWorktreeLandsOnMainBranch(t *testing.T) {
	requireCommitModeBullseye(t)
	clone := t.TempDir()
	initGitRepo(t, clone)
	wt := filepath.Join(clone, "wt")
	git(t, clone, "worktree", "add", wt)
	if err := worktreereap.Mark(&worktreereap.MarkArgs{Worktree: wt, Note: t.Name()}); err != nil {
		t.Fatal(err)
	}

	_, err := bullseyerun.RunMutation(
		"commit", "--op", "track", "--cwd", wt,
		"--name", "from worktree", "--acceptance", "lands on shared branch",
	)
	if err != nil {
		t.Fatal(err)
	}
	ledgerClean(t, clone)
	head, _ := os.ReadFile(filepath.Join(wt, "bullseye.yaml"))
	if !strings.Contains(string(head), "from worktree") {
		t.Fatalf("worktree ledger not mirrored from HEAD")
	}
	blob := gitOut(t, clone, "show", "HEAD:bullseye.yaml")
	if !strings.Contains(blob, "from worktree") {
		t.Fatalf("HEAD blob missing target")
	}
}

func TestT1151_2Concurrent50Writes(t *testing.T) {
	requireCommitModeBullseye(t)
	clone := t.TempDir()
	initGitRepo(t, clone)

	const writers = 50
	var wg sync.WaitGroup
	var fails atomic.Int32
	for i := range writers {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := fmt.Sprintf("T1151.2 concurrent %02d", n)
			_, err := bullseyerun.RunMutation(
				"commit", "--op", "track", "--cwd", clone,
				"--name", name, "--acceptance", "present in final ledger",
			)
			if err != nil {
				fails.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if fails.Load() > 0 {
		t.Fatalf("%d of %d writes failed", fails.Load(), writers)
	}
	blob := gitOut(t, clone, "show", "HEAD:bullseye.yaml")
	for i := range writers {
		want := fmt.Sprintf("T1151.2 concurrent %02d", i)
		if !strings.Contains(blob, want) {
			t.Fatalf("missing %q in final ledger", want)
		}
	}
	ledgerClean(t, clone)
}

func TestT1151_2AchieveSurvivesWorkingTreeReset(t *testing.T) {
	requireCommitModeBullseye(t)
	clone := t.TempDir()
	initGitRepo(t, clone)

	trackOut, err := bullseyerun.RunMutation(
		"commit", "--op", "track", "--cwd", clone,
		"--name", "reset survivor", "--acceptance", "ok",
	)
	if err != nil {
		t.Fatalf("track: %v\n%s", err, trackOut)
	}
	id := "T1"
	for _, line := range strings.Split(trackOut, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "ids:") {
			id = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "ids:"))
			break
		}
	}
	achOut, err := bullseyerun.RunMutation(
		"commit", "--op", "achieve", "--cwd", clone,
		"--id", id, "--attestation", "Hermetic oracle TestT1151_2AchieveSurvivesWorkingTreeReset: row remains on HEAD after git reset --hard clears the working tree copy.",
	)
	if err != nil {
		t.Fatalf("achieve: %v\n%s", err, achOut)
	}
	writeFile(t, filepath.Join(clone, "bullseye.yaml"), "targets: {}\n")
	git(t, clone, "reset", "--hard", "HEAD")
	blob := gitOut(t, clone, "show", "HEAD:bullseye.yaml")
	if !strings.Contains(blob, "status: achieved") || !strings.Contains(blob, "reset survivor") {
		t.Fatalf("achievement lost from HEAD after reset; blob=%q", blob)
	}
}

func TestT1151_2ConcurrentProcesses(t *testing.T) {
	requireCommitModeBullseye(t)
	clone := t.TempDir()
	initGitRepo(t, clone)

	const procs = 10
	var wg sync.WaitGroup
	var fails atomic.Int32
	for i := range procs {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				name := fmt.Sprintf("proc %d row %d", n, j)
				_, err := bullseyerun.RunMutation(
					"commit", "--op", "track", "--cwd", clone,
					"--name", name, "--acceptance", "ok",
				)
				if err != nil {
					fails.Add(1)
					return
				}
			}
		}(i)
	}
	wg.Wait()
	if fails.Load() > 0 {
		t.Fatalf("subprocess writers failed")
	}
	blob := gitOut(t, clone, "show", "HEAD:bullseye.yaml")
	for i := range procs {
		for j := 0; j < 5; j++ {
			want := fmt.Sprintf("proc %d row %d", i, j)
			if !strings.Contains(blob, want) {
				t.Fatalf("missing %q", want)
			}
		}
	}
}
