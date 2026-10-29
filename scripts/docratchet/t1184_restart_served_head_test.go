// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/t553oracle"
	"github.com/marcelocantos/jevons/internal/testreap"
	"github.com/marcelocantos/jevons/internal/worktreereap"
)

// 🎯T1184: the restart log's `served head=` is the commit of the binary
// actually running, not git HEAD of the shared clone. A worktree build
// can never pass as HEAD again.
func TestT1184ServedHeadReportsRunningBinaryTree(t *testing.T) {
	if _, err := os.Stat("/usr/sbin/lsof"); err != nil {
		t.Skip("lsof unavailable; T1184 served-head oracle needs it to name the running binary")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain unavailable for the fake daemon")
	}

	port := freeTCPPort(t)
	if port == 13705 {
		t.Fatalf("refusing development port %d", port)
	}
	testreap.Arm(t)

	main := t.TempDir()
	home := t.TempDir()
	srcDir := t.TempDir()
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	for _, d := range []string{
		filepath.Join(main, "scripts"),
		filepath.Join(main, "bin"),
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	script := filepath.Join(main, "scripts", "restart-jevonsd.sh")
	if err := os.WriteFile(script, []byte(readRepo(t, "scripts/restart-jevonsd.sh")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(main, "go.mod"),
		[]byte("module fixture\n\ngo 1.26\n\nrequire github.com/marcelocantos/claudia v0.24.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(main, "init", "-q")
	git(main, "add", "scripts/restart-jevonsd.sh", "go.mod")
	git(main, "commit", "-q", "-m", "shared seed")
	sharedHead, err := exec.Command("git", "-C", main, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	sharedSHA := strings.TrimSpace(string(sharedHead))

	wt := filepath.Join(t.TempDir(), "worker")
	git(main, "worktree", "add", "-q", "--detach", wt)
	if err := worktreereap.Mark(&worktreereap.MarkArgs{Worktree: wt, Note: t.Name()}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "worktree-only.txt"), []byte("worker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(wt, "add", "worktree-only.txt")
	git(wt, "commit", "-q", "-m", "worktree commit")
	wtHead, err := exec.Command("git", "-C", wt, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	wtSHA := strings.TrimSpace(string(wtHead))
	if wtSHA == sharedSHA {
		t.Fatal("worktree HEAD must differ from the shared clone")
	}

	for _, helper := range []string{"runlock", "detach", "claudiapin", "buildident"} {
		build := exec.Command("go", "build", "-o",
			filepath.Join(main, "bin", helper),
			filepath.Join(repoRoot(t), "cmd", helper))
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", helper, err, out)
		}
	}

	if err := os.WriteFile(filepath.Join(srcDir, "main.go"), []byte(fakeDaemonSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "go.mod"), []byte("module fakejevonsd\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(wt, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(wt, "bin", "jevonsd")
	build := exec.Command("go", "build", "-ldflags", "-X main.variant=wt", "-o", bin, ".")
	build.Dir = srcDir
	build.Env = append(os.Environ(), "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake daemon: %v\n%s", err, out)
	}
	t.Cleanup(func() { testreap.ReapUnder(t, wt) })

	servedFile := filepath.Join(home, "restart.served")
	cmd := exec.Command("/bin/bash", script)
	cmd.Dir = main
	cmd.Env = append(scrubRestartControlEnv(os.Environ()),
		"PATH=/usr/bin:/bin:/usr/sbin:/sbin",
		"HOME="+home,
		"JEVONS_RESTART_PORT="+strconv.Itoa(port),
		"JEVONS_RESTART_WORKDIR="+main,
		"JEVONS_RESTART_LOG="+filepath.Join(home, "daemon.log"),
		"JEVONS_RESTART_WAIT_SEC=30",
		"JEVONS_RESTART_STOP_WAIT_SEC=5",
		"JEVONS_RESTART_SKIP_MAKE=1",
		"JEVONS_RESTART_LOCK_WAIT_SEC=90",
		"JEVONS_RESTART_ACTIVE="+filepath.Join(home, "restart.active"),
		"JEVONS_RESTART_LOCK="+filepath.Join(home, "restart.lock"),
		"JEVONS_RESTART_BIN="+bin,
		"JEVONS_RESTART_SERVED="+servedFile,
		"JEVONS_RESTART_DETACH_LOG="+filepath.Join(home, "detach.log"),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("restart with worktree binary: %v\n%s", err, out)
	}
	got := string(out)
	if !strings.Contains(got, "OK: development jevonsd serving") {
		t.Fatalf("restart did not reach serving state:\n%s", got)
	}
	raw, err := os.ReadFile(servedFile)
	if err != nil {
		t.Fatalf("read served file: %v\n%s", err, got)
	}
	sha, err := t553oracle.ParseServedSHA(string(raw))
	if err != nil {
		t.Fatalf("parse served file %q: %v\nrestart:\n%s", raw, err, got)
	}
	if sha == sharedSHA {
		t.Fatalf("served head=%s is the shared clone; a worktree build passed as HEAD:\n%s\nrestart:\n%s", sha, raw, got)
	}
	if sha != wtSHA {
		t.Fatalf("served head=%s want worktree %s (shared was %s):\n%s\nrestart:\n%s", sha, wtSHA, sharedSHA, raw, got)
	}
	if !strings.Contains(got, "served head="+wtSHA[:12]) {
		t.Fatalf("restart log did not name the running binary's head %s:\n%s", wtSHA[:12], got)
	}
	if !strings.Contains(got, "🎯T1184") {
		t.Fatalf("restart must say it observed a foreign tree:\n%s", got)
	}
}
