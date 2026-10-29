// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleetgit_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/commitattrib"
	"github.com/marcelocantos/jevons/internal/fleetgit"
	"github.com/marcelocantos/jevons/internal/treeguard"
	"github.com/marcelocantos/jevons/internal/worktree"
	"github.com/marcelocantos/jevons/internal/worktreereap"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestRefuseStashPushNamesDirtyPathsInSharedClone(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "-q", "-b", "master")
	git(t, root, "config", "user.email", "t@example.com")
	git(t, root, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(root, "bullseye.yaml"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "bullseye.yaml")
	git(t, root, "commit", "-q", "-m", "seed")
	if err := os.WriteFile(filepath.Join(root, "bullseye.yaml"), []byte("seed\nT1133: filed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	refuse, msg, ok := fleetgit.RefuseStashPush(root, true)
	if !ok || !refuse {
		t.Fatalf("want refuse in shared clone with dirty ledger, got refuse=%v ok=%v", refuse, ok)
	}
	if !strings.Contains(msg, "bullseye.yaml") {
		t.Fatalf("refusal must name dirty paths: %q", msg)
	}
	if !strings.Contains(msg, "T1134") {
		t.Fatalf("refusal must cite target: %q", msg)
	}
}

func TestRefuseStashPushSkipsLinkedWorktree(t *testing.T) {
	org := t.TempDir()
	base := filepath.Join(org, "repo")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, base, "init", "-q", "-b", "master")
	git(t, base, "config", "user.email", "t@example.com")
	git(t, base, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(base, "f.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, base, "add", "f.txt")
	git(t, base, "commit", "-q", "-m", "seed")
	wt := filepath.Join(org, "wt")
	if out, err := exec.Command("git", "-C", base, "worktree", "add", "-b", "jv-x", wt).CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	// 🎯T440: a killed test run skips t.Cleanup; the reaper needs the mark.
	if err := worktreereap.Mark(&worktreereap.MarkArgs{Worktree: wt, Note: t.Name()}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "f.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	refuse, _, ok := fleetgit.RefuseStashPush(wt, true)
	if ok && refuse {
		t.Fatal("stash guard must not apply inside a worker's linked worktree")
	}
}

func TestRefuseStashPushSkipsNonFleetSeat(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "-q", "-b", "master")
	git(t, root, "config", "user.email", "t@example.com")
	git(t, root, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "f.txt")
	git(t, root, "commit", "-q", "-m", "seed")
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, ok := fleetgit.RefuseStashPush(root, false)
	if ok {
		t.Fatal("non-fleet seat: guard should not engage")
	}
}

func TestFleetSeatReadsActorEnv(t *testing.T) {
	t.Setenv(commitattrib.ActorEnv, "jv-t1134-auto")
	if !fleetgit.FleetSeat() {
		t.Fatal("expected fleet seat when JEVONS_AGENT is set")
	}
	t.Setenv(commitattrib.ActorEnv, "")
	if fleetgit.FleetSeat() {
		t.Fatal("expected not a fleet seat when JEVONS_AGENT is empty")
	}
}

func TestRefuseSharedCloneGitCommitFromWorktreeSeat(t *testing.T) {
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
	run("init", "-q")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(base, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "seed")
	wt, err := worktree.Ensure(base, "jv-t1151-seat")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(commitattrib.ActorEnv, "jv-t1151-seat")
	refuse, msg, ok := fleetgit.RefuseSharedCloneGitWrite(wt, base, true, "git -C "+base+" commit -m x")
	if !ok || !refuse || msg == "" {
		t.Fatalf("want refuse git -C shared from worktree seat, got refuse=%v ok=%v msg=%q", refuse, ok, msg)
	}
	if !strings.Contains(msg, "jv-t1151-seat") || !strings.Contains(msg, "commit") {
		t.Fatalf("refusal must name seat and command: %q", msg)
	}
}

func TestRefuseSharedCloneGitResetFromWorktreeSeat(t *testing.T) {
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
	run("init", "-q")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(base, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "seed")
	wt, err := worktree.Ensure(base, "jv-t1151-seat")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(commitattrib.ActorEnv, "jv-t1151-seat")
	cmd := "git -C " + base + " reset --hard"
	refuse, msg, ok := fleetgit.RefuseSharedCloneGitWrite(wt, base, true, cmd)
	if !ok || !refuse {
		t.Fatalf("want refuse reset --hard against shared clone, got refuse=%v ok=%v", refuse, ok)
	}
	if !strings.Contains(msg, "reset --hard") {
		t.Fatalf("refusal must quote command: %q", msg)
	}
}

func TestRefuseSharedCloneGitFromSeatInSharedClone(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "-q", "-b", "master")
	git(t, root, "config", "user.email", "t@example.com")
	git(t, root, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "f.txt")
	git(t, root, "commit", "-q", "-m", "seed")
	t.Setenv(commitattrib.ActorEnv, "jv-t1151-seat")
	refuse, msg, ok := fleetgit.RefuseSharedCloneGitWrite(root, root, true, "git reset --hard")
	if !ok || !refuse {
		t.Fatalf("want refuse reset in shared clone, got refuse=%v ok=%v", refuse, ok)
	}
	if !strings.Contains(msg, "reset --hard") {
		t.Fatalf("refusal must quote command: %q", msg)
	}
}

func TestRefuseForeignSharedCloneGitCommitFromWorktreeSeat(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	org := t.TempDir()
	jevons := filepath.Join(org, "jevons")
	claudia := filepath.Join(org, "claudia")
	for _, base := range []string{jevons, claudia} {
		if err := os.MkdirAll(base, 0o755); err != nil {
			t.Fatal(err)
		}
		git(t, base, "init", "-q", "-b", "master")
		git(t, base, "config", "user.email", "t@example.com")
		git(t, base, "config", "user.name", "t")
		if err := os.WriteFile(filepath.Join(base, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		git(t, base, "add", ".")
		git(t, base, "commit", "-q", "-m", "seed")
	}
	wt, err := worktree.Ensure(jevons, "jv-t1151.6-seat")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(commitattrib.ActorEnv, "jv-t1151.6-seat")
	refuse, msg, ok := fleetgit.RefuseSharedCloneGitWrite(wt, claudia, true, "git -C "+claudia+" commit")
	if !ok || !refuse {
		t.Fatalf("want refuse git -C sibling shared clone, got refuse=%v ok=%v msg=%q", refuse, ok, msg)
	}
	if !strings.Contains(msg, "claudia-po") || !strings.Contains(msg, "jevons_agent_send") {
		t.Fatalf("refusal must name sibling PO and send route: %q", msg)
	}
	if !strings.Contains(msg, "T1151.6") {
		t.Fatalf("refusal must cite target: %q", msg)
	}
}


func TestRefuseForeignSharedCloneGoBuildFromWorktreeSeat(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	org := t.TempDir()
	jevons := filepath.Join(org, "jevons")
	claudia := filepath.Join(org, "claudia")
	for _, base := range []string{jevons, claudia} {
		if err := os.MkdirAll(base, 0o755); err != nil {
			t.Fatal(err)
		}
		git(t, base, "init", "-q", "-b", "master")
		git(t, base, "config", "user.email", "t@example.com")
		git(t, base, "config", "user.name", "t")
		if err := os.WriteFile(filepath.Join(base, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		git(t, base, "add", ".")
		git(t, base, "commit", "-q", "-m", "seed")
	}
	wt, err := worktree.Ensure(jevons, "jv-t1151.8-seat")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(commitattrib.ActorEnv, "jv-t1151.8-seat")
	outPath := filepath.Join(claudia, "bin", "claudia")
	refuse, msg, ok := fleetgit.RefuseForeignSharedCloneAbsWrite(wt, outPath, "jv-t1151.8-seat", treeguard.FormGoBuildOut)
	if !ok || !refuse {
		t.Fatalf("want refuse go build -o in sibling clone, got refuse=%v ok=%v msg=%q", refuse, ok, msg)
	}
	if !strings.Contains(msg, "claudia-po") || !strings.Contains(msg, "T1151.8") || !strings.Contains(msg, "/tmp") {
		t.Fatalf("refusal must name PO, target, and private build path: %q", msg)
	}
	refuse, msg, ok = fleetgit.RefuseForeignSharedCloneAbsWrite(wt, filepath.Join(t.TempDir(), "x"), "jv-t1151.8-seat", treeguard.FormGoBuildOut)
	if ok && refuse {
		t.Fatalf("want allow build output outside sibling clone, got %q", msg)
	}
}
