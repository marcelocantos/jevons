// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T440's load-bearing oracle. The pure policy in worktreereap_test.go says
// what the sweeper decides; this file kills a real process that owns a real
// worktree in a real repository and asserts the entry actually disappears.
//
// The distinction matters because the fault being fixed is *how the owner
// dies*. Both ratchet tests already defer `worktree remove --force`, and both
// leaked anyway: a SIGKILL runs no defer, no t.Cleanup and no signal handler.
// A test that removed the worktree politely and then checked it was gone would
// pass against the very code that leaks. So the owner here is killed with
// SIGKILL and is given nothing to clean up with.
package worktreereap

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// tempRepo makes a real git repository with one commit, since every signal
// this package reads — the worktree list, the admin directory, the marker
// location — is git's own and cannot be faked convincingly.
func tempRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	// Resolved, because macOS hands out /var/… temp dirs that are symlinks
	// to /private/var/… and git reports the resolved form in every answer
	// this package reads. Comparing the two forms is a test bug that only
	// shows up once the directory is gone and cannot be resolved any more.
	root := resolve(t, t.TempDir())
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t440", "GIT_AUTHOR_EMAIL=t440@example.invalid",
			"GIT_COMMITTER_NAME=t440", "GIT_COMMITTER_EMAIL=t440@example.invalid",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "--quiet", "-b", "master")
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "file.txt")
	run("commit", "--quiet", "-m", "initial")
	return root
}

// addWorktree checks HEAD out into a detached worktree under a fresh temp dir,
// the way every verification rail in this repo does it.
func addWorktree(t *testing.T, root, name string) string {
	t.Helper()
	wt := filepath.Join(resolve(t, t.TempDir()), name)
	if out, err := exec.Command("git", "-C", root, "worktree", "add", "--detach", wt, "HEAD").CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}
	return wt
}

// resolve returns path with every symlink expanded, the form git reports.
func resolve(t *testing.T, path string) string {
	t.Helper()
	out, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("resolve %s: %v", path, err)
	}
	return out
}

// listed reports whether git still knows about wt in root.
func listed(t *testing.T, root, wt string) bool {
	t.Helper()
	out, err := exec.Command("git", "-C", root, "worktree", "list", "--porcelain").Output()
	if err != nil {
		t.Fatalf("git worktree list: %v", err)
	}
	return strings.Contains(string(out), wt)
}

// aggressive is a policy with no waiting period, so a test never sleeps out a
// grace window. The graces themselves are covered by the pure table; what this
// file exercises is the machinery around them.
func aggressive(root string) Config {
	return Config{Policy: Policy{
		RepoRoot:     root,
		SessionGrace: time.Nanosecond,
		IdleGrace:    time.Nanosecond,
	}}
}

// TestT440SweepReapsAWorktreeWhoseOwnerWasKilled is the acceptance oracle: a
// verification worktree whose creator is SIGKILLed mid-run is gone after one
// sweep, directory and administrative entry alike.
func TestT440SweepReapsAWorktreeWhoseOwnerWasKilled(t *testing.T) {
	root := tempRepo(t)
	wt := addWorktree(t, root, "head")

	// A real process stands in for the killed `go test` binary. It must be
	// something ps will report with a stable command line, and something we
	// can kill without a handler ever running.
	owner := exec.Command("sleep", "600")
	if err := owner.Start(); err != nil {
		t.Fatalf("start owner process: %v", err)
	}
	defer func() {
		_ = owner.Process.Kill()
		_ = owner.Wait()
	}()

	if err := Mark(&MarkArgs{Worktree: wt, PID: owner.Process.Pid, Note: "T440 oracle"}); err != nil {
		t.Fatalf("mark worktree: %v", err)
	}

	// THE CONTROL. Without it a sweeper that removed every worktree it saw
	// would pass the kill half of this test, and the assertion below would
	// be measuring nothing. While the owner runs, the tree stays — under
	// the same aggressive policy the kill half uses, so the only variable
	// between the two sweeps is whether the owner is alive.
	res, err := Sweep(aggressive(root))
	if err != nil {
		t.Fatalf("sweep with owner alive: %v", err)
	}
	if v := find(t, res, wt); v.Decision.Action != ActionKeep {
		t.Fatalf("a worktree whose owner (pid %d) is running was %s: %s\n%s",
			owner.Process.Pid, v.Decision.Action, v.Decision.Reason, res.Report())
	}
	if !listed(t, root, wt) {
		t.Fatalf("the control sweep removed a live owner's worktree")
	}

	// The fault, reproduced: SIGKILL, so no defer, no t.Cleanup, no handler.
	if err := owner.Process.Kill(); err != nil {
		t.Fatalf("kill owner: %v", err)
	}
	if _, err := owner.Process.Wait(); err != nil {
		t.Fatalf("reap owner: %v", err)
	}

	res, err = Sweep(aggressive(root))
	if err != nil {
		t.Fatalf("sweep after kill: %v", err)
	}
	if v := find(t, res, wt); !v.Removed {
		t.Fatalf("a worktree whose owner was killed was %s: %s\n%s",
			v.Decision.Action, v.Decision.Reason, res.Report())
	}
	if listed(t, root, wt) {
		t.Fatalf("`git worktree list` still names %s after the sweep", wt)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("the worktree directory %s survived the sweep (stat err: %v) — "+
			"`git worktree prune` alone leaves exactly this, which is why it is not the fix", wt, err)
	}
}

// TestT440SweepHoldsAWorktreeWithModifiedTrackedFiles: whatever the owner
// looks like, a tree somebody edited in is reported rather than deleted. The
// held entry must say why, or a survivor is indistinguishable from a bug.
func TestT440SweepHoldsAWorktreeWithModifiedTrackedFiles(t *testing.T) {
	root := tempRepo(t)
	wt := addWorktree(t, root, "head")
	if err := os.WriteFile(filepath.Join(wt, "file.txt"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// An owner that never existed: pid 1 is init, which is running but is
	// certainly not `go test`, so the marker resolves to "owner is gone".
	if err := Mark(&MarkArgs{Worktree: wt, PID: 1, Note: "T440 oracle"}); err != nil {
		t.Fatalf("mark worktree: %v", err)
	}
	// Overwrite the recorded command so pid 1 cannot match it.
	writeMarkerCmd(t, wt, "go test ./scripts/docratchet")

	res, err := Sweep(aggressive(root))
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	v := find(t, res, wt)
	if v.Decision.Action != ActionHold {
		t.Fatalf("a dirty worktree was %s: %s", v.Decision.Action, v.Decision.Reason)
	}
	if !strings.Contains(v.Decision.Reason, "modified tracked files") {
		t.Fatalf("the hold reason does not name the modification: %s", v.Decision.Reason)
	}
	if !listed(t, root, wt) {
		t.Fatalf("a dirty worktree was removed anyway")
	}
}

// TestT440SweepIgnoresUntrackedBuildOutput: the previous test's guard must not
// fire on the ordinary contents of a verification worktree, or every entry the
// sweeper exists to remove would be held forever.
func TestT440SweepIgnoresUntrackedBuildOutput(t *testing.T) {
	root := tempRepo(t)
	wt := addWorktree(t, root, "head")
	if err := os.MkdirAll(filepath.Join(wt, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "bin", "jevonsd"), []byte("ELF"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Mark(&MarkArgs{Worktree: wt, PID: 1}); err != nil {
		t.Fatalf("mark worktree: %v", err)
	}
	writeMarkerCmd(t, wt, "go test ./scripts/docratchet")

	res, err := Sweep(aggressive(root))
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if v := find(t, res, wt); !v.Removed {
		t.Fatalf("untracked build output held a dead owner's worktree: %s — %s", v.Decision.Action, v.Decision.Reason)
	}
}

// TestT440SweepNeverRemovesProtectedOrMain: the daemon's build snapshot and
// the clone itself are the two entries that must survive every sweep. Both are
// declared, not inferred, because inferring them is how a reaper eventually
// eats one.
func TestT440SweepNeverRemovesProtectedOrMain(t *testing.T) {
	root := tempRepo(t)
	snap := addWorktree(t, root, "build-snapshot")
	if err := Mark(&MarkArgs{Worktree: snap, PID: 1}); err != nil {
		t.Fatalf("mark worktree: %v", err)
	}
	writeMarkerCmd(t, snap, "a process that is long gone")

	cfg := aggressive(root)
	cfg.Protected = []string{snap}
	res, err := Sweep(cfg)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if v := find(t, res, snap); v.Decision.Action != ActionKeep {
		t.Fatalf("the protected snapshot was %s: %s", v.Decision.Action, v.Decision.Reason)
	}
	if v := find(t, res, root); v.Decision.Action != ActionKeep {
		t.Fatalf("the clone itself was %s: %s", v.Decision.Action, v.Decision.Reason)
	}
	if !listed(t, root, snap) {
		t.Fatalf("the protected snapshot was removed")
	}
}

// TestT440DryRunRemovesNothing: the report a dry run prints is the report the
// real sweep would print, which is what makes it worth reading first.
func TestT440DryRunRemovesNothing(t *testing.T) {
	root := tempRepo(t)
	wt := addWorktree(t, root, "head")
	if err := Mark(&MarkArgs{Worktree: wt, PID: 1}); err != nil {
		t.Fatalf("mark worktree: %v", err)
	}
	writeMarkerCmd(t, wt, "go test ./scripts/docratchet")

	cfg := aggressive(root)
	cfg.DryRun = true
	res, err := Sweep(cfg)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	v := find(t, res, wt)
	if v.Decision.Action != ActionRemove {
		t.Fatalf("dry run decided %s, want remove", v.Decision.Action)
	}
	if v.Removed || res.Removed != 0 {
		t.Fatalf("dry run removed %d worktrees", res.Removed)
	}
	if !listed(t, root, wt) {
		t.Fatalf("dry run deleted %s", wt)
	}
	if !strings.Contains(res.Report(), "would remove") {
		t.Fatalf("the dry-run report does not say what it would do:\n%s", res.Report())
	}
}

// TestT440MarkerDiesWithItsWorktree: git deletes the admin directory when a
// worktree is removed, so a marker cannot outlive what it describes and be
// read as evidence about a path that has since been reused.
func TestT440MarkerDiesWithItsWorktree(t *testing.T) {
	root := tempRepo(t)
	wt := addWorktree(t, root, "head")
	if err := Mark(&MarkArgs{Worktree: wt, PID: os.Getpid()}); err != nil {
		t.Fatalf("mark worktree: %v", err)
	}
	common, err := git(root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := readMarkers(common)[filepath.Clean(wt)]; !ok {
		t.Fatalf("the marker just written was not read back")
	}
	if out, err := exec.Command("git", "-C", root, "worktree", "remove", "--force", wt).CombinedOutput(); err != nil {
		t.Fatalf("git worktree remove: %v\n%s", err, out)
	}
	if _, ok := readMarkers(common)[filepath.Clean(wt)]; ok {
		t.Fatalf("the marker outlived its worktree")
	}
}

// find returns the verdict for one path, failing the test if the sweep did not
// see it at all — an absent entry would otherwise read as a silent pass.
func find(t *testing.T, res Result, path string) Verdict {
	t.Helper()
	want := filepath.Clean(path)
	for _, v := range res.Verdicts {
		if v.Entry.Path == want {
			return v
		}
	}
	t.Fatalf("the sweep never saw %s\n%s", path, res.Report())
	return Verdict{}
}

// writeMarkerCmd rewrites the recorded command line of an existing marker, so
// a test can make a live pid fail the identity check without needing a process
// it is allowed to kill.
func writeMarkerCmd(t *testing.T, wt, cmd string) {
	t.Helper()
	admin, err := adminDir(wt)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(admin, markerName)
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m Marker
	if err := json.Unmarshal(blob, &m); err != nil {
		t.Fatal(err)
	}
	m.Cmd = cmd
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
}
