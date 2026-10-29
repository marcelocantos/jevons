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

// 🎯T1184: run from a linked worktree with the production config dir, the
// supervisor installer hands off to the main working tree's installer (a stub
// here), so the rendered configs can never name a worker's tree.
func TestT1184SupervisorInstallFromWorktreeHandsOffToSharedClone(t *testing.T) {
	real := readRepo(t, "supervisor/install.sh")
	main := t.TempDir()
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	script := filepath.Join(main, "supervisor", "install.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte(real), 0o755); err != nil {
		t.Fatal(err)
	}
	git(main, "init", "-q")
	git(main, "add", "supervisor/install.sh")
	git(main, "commit", "-q", "-m", "seed")
	wt := filepath.Join(t.TempDir(), "worker")
	git(main, "worktree", "add", "-q", "--detach", wt)
	if err := worktreereap.Mark(&worktreereap.MarkArgs{Worktree: wt, Note: t.Name()}); err != nil {
		t.Fatal(err)
	}
	stub := "#!/bin/sh\necho \"MAIN-TREE-INSTALL root=$(cd \"$(dirname \"$0\")/..\" && pwd)\"\n"
	if err := os.WriteFile(script, []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("/bin/sh", filepath.Join(wt, "supervisor", "install.sh"))
	cmd.Env = withoutEnv(os.Environ(), "SUPERVISOR_CONF_DIR")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install from worktree: %v\n%s", err, out)
	}
	got := string(out)
	mainReal, _ := filepath.EvalSymlinks(main)
	if !strings.Contains(got, "MAIN-TREE-INSTALL") || !(strings.Contains(got, "root="+mainReal) || strings.Contains(got, "root="+main)) {
		t.Fatalf("worktree install did not hand off to the shared clone's installer:\n%s", got)
	}
	if !strings.Contains(got, "🎯T1184") {
		t.Fatalf("hand-off must say why:\n%s", got)
	}
}

// 🎯T1184: a test SUPERVISOR_CONF_DIR is unaffected — the worktree's own
// installer renders locally instead of handing off to the shared clone.
func TestT1184SupervisorInstallFromWorktreeWithTestDirRendersLocally(t *testing.T) {
	main := t.TempDir()
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	sup := filepath.Join(main, "supervisor")
	if err := os.MkdirAll(sup, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sup, "install.sh"), []byte(readRepo(t, "supervisor/install.sh")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sup, "jevonsd.ini"), []byte(readRepo(t, "supervisor/jevonsd.ini")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sup, "run-jevonsd.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	git(main, "init", "-q")
	git(main, "add", "supervisor/install.sh", "supervisor/jevonsd.ini", "supervisor/run-jevonsd.sh")
	git(main, "commit", "-q", "-m", "seed")
	wt := filepath.Join(t.TempDir(), "worker")
	git(main, "worktree", "add", "-q", "--detach", wt)
	if err := worktreereap.Mark(&worktreereap.MarkArgs{Worktree: wt, Note: t.Name()}); err != nil {
		t.Fatal(err)
	}

	conf := t.TempDir()
	home := t.TempDir()
	cmd := exec.Command("/bin/sh", filepath.Join(wt, "supervisor", "install.sh"))
	cmd.Env = append(withoutEnv(os.Environ(), "SUPERVISOR_CONF_DIR"),
		"SUPERVISOR_CONF_DIR="+conf,
		"SUPERVISOR_SKIP_CTL=1",
		"SUPERVISOR_SKIP_LAUNCHAGENT=1",
		"HOME="+home,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install from worktree with test dir: %v\n%s", err, out)
	}
	got := string(out)
	if strings.Contains(got, "🎯T1184") {
		t.Fatalf("test SUPERVISOR_CONF_DIR must not hand off to the shared clone:\n%s", got)
	}
	body, err := os.ReadFile(filepath.Join(conf, "jevonsd.ini"))
	if err != nil {
		t.Fatalf("expected local render into %s: %v\n%s", conf, err, got)
	}
	rendered := string(body)
	wtReal, _ := filepath.EvalSymlinks(wt)
	mainReal, _ := filepath.EvalSymlinks(main)
	if !strings.Contains(rendered, wt) && !strings.Contains(rendered, wtReal) {
		t.Fatalf("test-dir render did not name the worktree (%s):\n%s", wt, rendered)
	}
	if strings.Contains(rendered, main) || (mainReal != "" && mainReal != wtReal && strings.Contains(rendered, mainReal)) {
		t.Fatalf("test-dir render named the shared clone instead of the worktree:\n%s", rendered)
	}
}

func TestT1184RestartScriptObservesRunningBinary(t *testing.T) {
	body := readRepo(t, "scripts/restart-jevonsd.sh")
	for _, m := range []string{
		"🎯T1184",
		"running_binary_tree",
		"worktree build can never pass as HEAD",
	} {
		if !strings.Contains(body, m) {
			t.Errorf("restart script missing T1184 marker %q", m)
		}
	}
}
