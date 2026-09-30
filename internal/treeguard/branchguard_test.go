package treeguard

// 🎯T955: a worker's landing must never switch the shared clone's checkout,
// and must never land on a branch other than the integration branch. The
// 2026-09-30 incident: jv-t946-resume2 ran `git checkout master` directly in
// the shared clone while the live integration branch was
// steer-modes-stop-guards-seat-stops, then fast-forwarded the wrong branch.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDetectBranchCheckout(t *testing.T) {
	cases := []struct {
		name         string
		command      string
		wantRef      string
		wantOK       bool
		wantDefinite bool
	}{
		{"bare checkout of a branch", "git checkout master", "master", true, false},
		{"switch is always definite", "git switch master", "master", true, true},
		{"checkout -b creates and switches", "git checkout -b feature/x", "feature/x", true, true},
		{"switch -c creates and switches", "git switch -c feature/x", "feature/x", true, true},
		{"path restore with --", "git checkout -- some/file.go", "", false, false},
		{"ref-and-path restore with --", "git checkout HEAD -- some/file.go", "", false, false},
		{"bare checkout with no args", "git checkout", "", false, false},
		{"restore subcommand is not a branch switch", "git restore some/file.go", "", false, false},
		{"unrelated command", "go test ./...", "", false, false},
		{"git status is read-only", "git status", "", false, false},
		{"second command in a pipeline", "make test && git checkout master", "master", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ref, ok, definite := DetectBranchCheckout(tc.command)
			if ref != tc.wantRef || ok != tc.wantOK || definite != tc.wantDefinite {
				t.Errorf("DetectBranchCheckout(%q) = (%q, %v, %v), want (%q, %v, %v)",
					tc.command, ref, ok, definite, tc.wantRef, tc.wantOK, tc.wantDefinite)
			}
		})
	}
}

func TestDecideBranchCheckout(t *testing.T) {
	allow := DecideBranchCheckout(&BranchGuardArgs{CurrentBranch: "steer", TargetRef: "steer"})
	if allow.Verdict != Allow {
		t.Fatalf("same-branch checkout = %+v, want Allow", allow)
	}

	deny := DecideBranchCheckout(&BranchGuardArgs{CurrentBranch: "steer", TargetRef: "master"})
	if deny.Verdict != Deny {
		t.Fatalf("cross-branch checkout = %+v, want Deny", deny)
	}
	for _, want := range []string{"steer", "master", "shared clone", "T955"} {
		if !strings.Contains(deny.Message, want) {
			t.Errorf("refusal message %q does not mention %q", deny.Message, want)
		}
	}
}

// runGit runs git in dir and fails the test on error.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// sharedClone builds a throwaway repo checked out on branch "steer", the
// fixture for the hermetic acceptance oracle (🎯T955 bullet 3).
func sharedClone(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "steer")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("steer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "steer base")
	runGit(t, dir, "branch", "master")
	return dir
}

func bashPayload(cwd, command string) *Payload {
	p := &Payload{SessionID: "fixture-worker", CWD: cwd, ToolName: ToolBash}
	p.ToolInput.Command = command
	return p
}

// TestHermeticFixtureWorkerCannotSwitchSharedCloneBranch is the acceptance
// oracle's third bullet: a fixture worker on a throwaway clone whose HEAD is
// `steer` attempts `git checkout master` plus a fast-forward merge; the guard
// refuses, and the clone's HEAD still reads `steer`.
func TestHermeticFixtureWorkerCannotSwitchSharedCloneBranch(t *testing.T) {
	dir := sharedClone(t)

	env := &Env{Store: &Store{Root: t.TempDir()}, RepoRoot: dir, Now: time.Now}
	got, err := env.Pre(bashPayload(dir, "git checkout master && git merge --ff-only jv-t946-resume2"))
	if err != nil {
		t.Fatalf("Pre: %v", err)
	}
	if got.Verdict != Deny {
		t.Fatalf("Pre(git checkout master) = %+v, want Deny", got)
	}
	for _, want := range []string{"steer", "master", "T955"} {
		if !strings.Contains(got.Message, want) {
			t.Errorf("refusal message %q does not mention %q", got.Message, want)
		}
	}

	// The clone's HEAD still reads `steer` — the guard only decides, it never
	// lets the shell run, so this is really just confirming nothing else in
	// the test touched the checkout.
	if branch := runGit(t, dir, "symbolic-ref", "--quiet", "--short", "HEAD"); branch != "steer" {
		t.Fatalf("shared clone HEAD moved to %q, want steer", branch)
	}
}

func TestDecideBranchCheckoutAllowsSwitchInsideWorkerOwnWorktree(t *testing.T) {
	base := sharedClone(t)
	// A linked worktree, on its own branch, switching to yet another local
	// branch: not the shared clone, so this guard has nothing to say.
	runGit(t, base, "branch", "jevons-worktree/jv-x")
	wt := filepath.Join(t.TempDir(), "wt")
	runGit(t, base, "worktree", "add", "-q", wt, "jevons-worktree/jv-x")

	env := &Env{Store: &Store{Root: t.TempDir()}, RepoRoot: wt, Now: time.Now}
	got, err := env.Pre(bashPayload(wt, "git checkout master"))
	if err != nil {
		t.Fatalf("Pre: %v", err)
	}
	if got.Verdict != Allow {
		t.Fatalf("Pre(git checkout master) in worker's own worktree = %+v, want Allow", got)
	}
}

func TestDecideBranchCheckoutAllowsPathRestoreInSharedClone(t *testing.T) {
	dir := sharedClone(t)
	env := &Env{Store: &Store{Root: t.TempDir()}, RepoRoot: dir, Now: time.Now}
	for _, cmd := range []string{
		"git checkout .",
		"git checkout -- README.md",
		"git checkout HEAD -- README.md",
	} {
		got, err := env.Pre(bashPayload(dir, cmd))
		if err != nil {
			t.Fatalf("Pre(%q): %v", cmd, err)
		}
		if got.Verdict != Allow {
			t.Errorf("Pre(%q) = %+v, want Allow (path restore, not a branch switch)", cmd, got)
		}
	}
}
