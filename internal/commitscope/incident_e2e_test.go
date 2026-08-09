// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitscope_test

// End-to-end oracle for 🎯T377, over a real git repository and the real
// scripts/hooks/pre-commit shim.
//
// It replays the incident that produced 29e69e8: worker A stages its work,
// worker B commits its own, and B's commit arrives carrying A's paths under
// a message describing B's. Nothing in the pure policy tests can catch that,
// because the bug lives in what git does with a shared index rather than in
// any decision the product makes.

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// workerA and workerB are the two concurrent fleet workers. Their files
// stand in for the real ones: fleet_cycle.js (A, 🎯T370) was swept into a
// commit about chat_wire.go (B, 🎯T372).
const (
	workerAPath = "fleet_cycle.js"
	workerBPath = "chat_wire.go"
)

// TestBareCommitCannotSweepAnotherWorkersStagedHunks is the incident itself.
func TestBareCommitCannotSweepAnotherWorkersStagedHunks(t *testing.T) {
	repo := newGuardedRepo(t)
	base := repo.head(t)

	repo.stageAs(t, "worker A", workerAPath, "A's 🎯T370 work")
	repo.write(t, workerBPath, "B's 🎯T372 work")

	out, err := repo.git(t, "add", workerBPath)
	if err != nil {
		t.Fatalf("worker B staging its own file: %v\n%s", err, out)
	}
	out, err = repo.git(t, "commit", "-m", "refactor(web): T372 one pending-owner-turn contract")
	if err == nil {
		t.Fatalf("bare commit succeeded; it committed:\n%s", repo.showStat(t, "HEAD"))
	}
	if !strings.Contains(out, workerAPath) {
		t.Errorf("refusal does not name the foreign path %q that would have been swept:\n%s", workerAPath, out)
	}
	if got := repo.head(t); got != base {
		t.Errorf("HEAD moved to %s despite the refusal (was %s)", got, base)
	}

	// Worker A's staging must survive the refusal untouched — a guard that
	// resolves the collision by discarding one side is the T376 bug wearing
	// a different hat.
	if staged := repo.stagedPaths(t); !slices.Contains(staged, workerAPath) {
		t.Errorf("worker A's staged hunk was lost; index holds %v", staged)
	}

	// And the scoped form, which is what the worker is told to run, works
	// and lands only worker B's file.
	if out, err := repo.git(t, "commit", "--only", workerBPath, "-m",
		"refactor(web): T372 one pending-owner-turn contract"); err != nil {
		t.Fatalf("git commit --only was refused:\n%s", out)
	}
	stat := repo.showStat(t, "HEAD")
	if !strings.Contains(stat, workerBPath) {
		t.Errorf("scoped commit is missing worker B's own file:\n%s", stat)
	}
	if strings.Contains(stat, workerAPath) {
		t.Errorf("scoped commit still swept worker A's file:\n%s", stat)
	}
	if staged := repo.stagedPaths(t); !slices.Contains(staged, workerAPath) {
		t.Errorf("worker A's staged hunk did not survive worker B's commit; index holds %v", staged)
	}
}

// TestEveryWholeIndexCommitFormIsRefused covers the other two spellings of
// "commit whatever is lying around". `-a` and `-i` reach the same tree as a
// bare commit, so a guard that only knew about the bare form would be a
// one-line detour.
func TestEveryWholeIndexCommitFormIsRefused(t *testing.T) {
	for _, form := range []struct {
		name string
		args []string
	}{
		{"bare", []string{"commit", "-m", "B"}},
		{"dash-a", []string{"commit", "-a", "-m", "B"}},
		{"dash-i", []string{"commit", "-i", workerBPath, "-m", "B"}},
	} {
		t.Run(form.name, func(t *testing.T) {
			repo := newGuardedRepo(t)
			base := repo.head(t)
			repo.stageAs(t, "worker A", workerAPath, "A's work")
			repo.write(t, workerBPath, "B's work")
			if out, err := repo.git(t, "add", workerBPath); err != nil {
				t.Fatalf("staging: %v\n%s", err, out)
			}
			out, err := repo.git(t, form.args...)
			if err == nil {
				t.Fatalf("git %s committed:\n%s", strings.Join(form.args, " "), repo.showStat(t, "HEAD"))
			}
			if !strings.Contains(out, "🎯T377") {
				t.Errorf("refusal is not attributable to the guard:\n%s", out)
			}
			if got := repo.head(t); got != base {
				t.Errorf("HEAD moved despite refusal")
			}
		})
	}
}

// TestPrivateIndexCommitIsAllowed. The other sanctioned mechanism: a worker
// that owns its index cannot pick up anyone else's staging, so the guard
// must not stand in its way.
func TestPrivateIndexCommitIsAllowed(t *testing.T) {
	repo := newGuardedRepo(t)
	repo.stageAs(t, "worker A", workerAPath, "A's work")
	repo.write(t, workerBPath, "B's work")

	private := filepath.Join(repo.dir, ".git", "index-jv-t377")
	priv := func(args ...string) string {
		t.Helper()
		out, err := repo.gitEnv(t, []string{"GIT_INDEX_FILE=" + private}, args...)
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return out
	}
	priv("read-tree", "HEAD")
	priv("update-index", "--add", workerBPath)
	priv("commit", "-m", "B, from its own index")

	stat := repo.showStat(t, "HEAD")
	if !strings.Contains(stat, workerBPath) || strings.Contains(stat, workerAPath) {
		t.Errorf("private-index commit did not contain exactly worker B's file:\n%s", stat)
	}
}

// TestNothingStagedIsNotRefused keeps the guard off commits that cannot
// misattribute anything, so `--amend` of a message and empty commits still
// work in a tree where another worker happens to be staging.
func TestNothingStagedIsNotRefused(t *testing.T) {
	repo := newGuardedRepo(t)
	if out, err := repo.git(t, "commit", "--allow-empty", "-m", "no content"); err != nil {
		t.Fatalf("empty commit was refused:\n%s", out)
	}
}

// TestExplicitOptOutIsHonoured. The owner committing by hand in a tree with
// no fleet workers in it is a single actor; the escape must exist and must
// have to be typed.
func TestExplicitOptOutIsHonoured(t *testing.T) {
	repo := newGuardedRepo(t)
	repo.stageAs(t, "worker A", workerAPath, "A's work")
	out, err := repo.gitEnv(t, []string{"JEVONS_COMMIT_SCOPE=off"}, "commit", "-m", "deliberate whole-index commit")
	if err != nil {
		t.Fatalf("opt-out commit was still refused:\n%s", out)
	}
}

// --- harness -------------------------------------------------------------

type guardedRepo struct {
	dir string
	bin string
}

// newGuardedRepo builds a two-file repository with the real pre-commit shim
// installed, isolated from the developer's own git configuration.
func newGuardedRepo(t *testing.T) *guardedRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	r := &guardedRepo{dir: t.TempDir(), bin: guardBinary(t)}

	if out, err := r.git(t, "init", "-q", "-b", "master", "."); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	for _, kv := range [][2]string{{"user.email", "t@example.invalid"}, {"user.name", "t"}} {
		if out, err := r.git(t, "config", kv[0], kv[1]); err != nil {
			t.Fatalf("git config: %v\n%s", err, out)
		}
	}

	hook := filepath.Join(r.dir, ".git", "hooks", "pre-commit")
	if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	shim, err := os.ReadFile(filepath.Join(repoRoot(t), "scripts", "hooks", "pre-commit"))
	if err != nil {
		t.Fatalf("reading the shipped shim: %v", err)
	}
	if err := os.WriteFile(hook, shim, 0o755); err != nil {
		t.Fatal(err)
	}

	r.write(t, workerAPath, "base")
	r.write(t, workerBPath, "base")
	if out, err := r.gitEnv(t, []string{"JEVONS_COMMIT_SCOPE=off"}, "add", "."); err != nil {
		t.Fatalf("seed add: %v\n%s", err, out)
	}
	if out, err := r.gitEnv(t, []string{"JEVONS_COMMIT_SCOPE=off"}, "commit", "-m", "base"); err != nil {
		t.Fatalf("seed commit: %v\n%s", err, out)
	}
	return r
}

func (r *guardedRepo) write(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(r.dir, name), []byte(content+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// stageAs is the concurrent worker: it edits its own file and stages it,
// then leaves, exactly as a fleet worker does between its edit and its commit.
func (r *guardedRepo) stageAs(t *testing.T, who, name, content string) {
	t.Helper()
	r.write(t, name, content)
	if out, err := r.git(t, "add", name); err != nil {
		t.Fatalf("%s staging %s: %v\n%s", who, name, err, out)
	}
}

func (r *guardedRepo) git(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return r.gitEnv(t, nil, args...)
}

// gitEnv runs git with a hermetic environment: the developer's global and
// system git config, and any GIT_INDEX_FILE this test process inherited,
// must not reach the hook under test.
func (r *guardedRepo) gitEnv(t *testing.T, extra []string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	cmd.Env = append([]string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + r.dir,
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
		"JEVONS_COMMITSCOPE_BIN=" + r.bin,
	}, extra...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (r *guardedRepo) head(t *testing.T) string {
	t.Helper()
	out, err := r.git(t, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v\n%s", err, out)
	}
	return strings.TrimSpace(out)
}

func (r *guardedRepo) showStat(t *testing.T, rev string) string {
	t.Helper()
	out, err := r.git(t, "show", "--stat", "--format=", rev)
	if err != nil {
		t.Fatalf("show --stat: %v\n%s", err, out)
	}
	return out
}

func (r *guardedRepo) stagedPaths(t *testing.T) []string {
	t.Helper()
	out, err := r.git(t, "diff", "--cached", "--name-only")
	if err != nil {
		t.Fatalf("diff --cached: %v\n%s", err, out)
	}
	return strings.Fields(out)
}

var (
	buildOnce sync.Once
	builtBin  string
	buildErr  error
)

// guardBinary compiles cmd/commitscope once for the whole package run. The
// tests exercise the shipped shim, and the shim execs this.
func guardBinary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "commitscope-bin")
		if err != nil {
			buildErr = err
			return
		}
		builtBin = filepath.Join(dir, "commitscope")
		cmd := exec.Command("go", "build", "-o", builtBin, "./cmd/commitscope")
		cmd.Dir = repoRoot(t)
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = err
			t.Logf("go build: %s", out)
		}
	})
	if buildErr != nil {
		t.Fatalf("building cmd/commitscope: %v", buildErr)
	}
	return builtBin
}

// repoRoot walks up from the package directory to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test's working directory")
		}
		dir = parent
	}
}
