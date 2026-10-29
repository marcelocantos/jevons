package treeguard

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/commitattrib"
	"github.com/marcelocantos/jevons/internal/worktree"
)

func TestT1151_8RefusesGoBuildInSiblingSharedClone(t *testing.T) {
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
		runGit(t, base, "init", "-q", "-b", "master")
		runGit(t, base, "config", "user.email", "t@example.com")
		runGit(t, base, "config", "user.name", "t")
		if err := os.WriteFile(filepath.Join(base, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runGit(t, base, "add", ".")
		runGit(t, base, "commit", "-q", "-m", "seed")
	}
	wt, err := worktree.Ensure(jevons, "jv-t1151.8-seat")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(commitattrib.ActorEnv, "jv-t1151.8-seat")
	env := &Env{Store: &Store{Root: t.TempDir()}, RepoRoot: wt, Now: time.Now}
	cmd := "cd " + claudia + " && go build -o bin/claudia ."
	got, err := env.Pre(bashPayload(wt, cmd))
	if err != nil {
		t.Fatalf("Pre: %v", err)
	}
	if got.Verdict != Deny {
		t.Fatalf("Pre(sibling go build) = %+v, want Deny", got)
	}
	if !strings.Contains(got.Message, "claudia-po") || !strings.Contains(got.Message, "T1151.8") {
		t.Fatalf("refusal must name sibling PO and target: %q", got.Message)
	}
}

func TestT1151_8AllowsGoBuildToPrivatePathFromWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	org := t.TempDir()
	claudia := filepath.Join(org, "claudia")
	if err := os.MkdirAll(claudia, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, claudia, "init", "-q", "-b", "master")
	runGit(t, claudia, "config", "user.email", "t@example.com")
	runGit(t, claudia, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(claudia, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, claudia, "add", ".")
	runGit(t, claudia, "commit", "-q", "-m", "seed")
	wt, err := worktree.Ensure(claudia, "jv-t1151.8-claudia-seat")
	if err != nil {
		t.Fatal(err)
	}
	tmpOut := filepath.Join(t.TempDir(), "claudia-bin")
	t.Setenv(commitattrib.ActorEnv, "jv-t1151.8-claudia-seat")
	env := &Env{Store: &Store{Root: t.TempDir()}, RepoRoot: wt, Now: time.Now}
	got, err := env.Pre(bashPayload(wt, "go build -o "+tmpOut+" ."))
	if err != nil {
		t.Fatalf("Pre: %v", err)
	}
	if got.Verdict == Deny {
		t.Fatalf("Pre(go build -o /tmp) should allow: %+v", got)
	}
}

func TestT1151_8AllowsGoTestInSiblingClone(t *testing.T) {
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
		runGit(t, base, "init", "-q", "-b", "master")
		runGit(t, base, "config", "user.email", "t@example.com")
		runGit(t, base, "config", "user.name", "t")
		if err := os.WriteFile(filepath.Join(base, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runGit(t, base, "add", ".")
		runGit(t, base, "commit", "-q", "-m", "seed")
	}
	wt, err := worktree.Ensure(jevons, "jv-t1151.8-seat")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(commitattrib.ActorEnv, "jv-t1151.8-seat")
	env := &Env{Store: &Store{Root: t.TempDir()}, RepoRoot: wt, Now: time.Now}
	got, err := env.Pre(bashPayload(wt, "cd "+claudia+" && go test ./..."))
	if err != nil {
		t.Fatalf("Pre: %v", err)
	}
	if got.Verdict == Deny {
		t.Fatalf("go test without -o should stay allowed: %+v", got)
	}
}
