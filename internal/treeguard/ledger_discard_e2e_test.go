package treeguard

// 🎯T1011: a `git reset --hard` (or path-less `git clean -f`) run in a repo
// whose bullseye.yaml has an uncommitted change must never silently discard
// it. The 2026-10-06 incident was exactly this shape — a `git reset: moving
// to HEAD` (a no-op ref move to the same SHA) that nonetheless cleared the
// working tree and wiped the T1012/T1013-family/T1014/T1015 ledger rows,
// because none of them had been committed yet.
//
// This test reproduces the incident in a throwaway repo: write bullseye.yaml
// without committing, then attempt the discarding command through the same
// Pre() entry point Claude Code's PreToolUse hook calls. The guard must deny
// while the ledger is dirty, and allow once it is committed.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ledgerRepo builds a throwaway repo with a committed bullseye.yaml, mirroring
// the shared clone's shape (ledger tracked at the repo root from commit one).
func ledgerRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "master")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"),
		[]byte("schema_version: 5\ntargets: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "init ledger")
	return dir
}

func TestGitResetHardRefusedWhileLedgerDirty(t *testing.T) {
	dir := ledgerRepo(t)

	// Reproduce the incident: a bullseye_* mutation writes the file but the
	// caller never commits it before the reset.
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"),
		[]byte("schema_version: 5\ntargets: {T1012: {status: achieved}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	env := &Env{Store: &Store{Root: t.TempDir()}, RepoRoot: dir, Now: time.Now}
	got, err := env.Pre(bashPayload(dir, "git reset --hard"))
	if err != nil {
		t.Fatalf("Pre: %v", err)
	}
	if got.Verdict != Deny {
		t.Fatalf("Pre(git reset --hard) with dirty ledger = %+v, want Deny", got)
	}
	for _, want := range []string{"bullseye.yaml", "T1011", "uncommitted"} {
		if !strings.Contains(got.Message, want) {
			t.Errorf("refusal message %q does not mention %q", got.Message, want)
		}
	}

	// The guard only advises; it does not itself run the command. Prove the
	// ledger mutation really would have been lost had the reset gone ahead,
	// so the test is pinned to the actual incident mechanism and not just a
	// policy string.
	before, err := os.ReadFile(filepath.Join(dir, "bullseye.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "reset", "--hard")
	after, err := os.ReadFile(filepath.Join(dir, "bullseye.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) == string(after) {
		t.Fatal("fixture invalid: git reset --hard did not actually discard the dirty ledger — the guard would be proving nothing")
	}
}

func TestGitResetHardAllowedWhenLedgerCommitted(t *testing.T) {
	dir := ledgerRepo(t)
	// Mutate AND commit, same turn — the discipline this target wants to make
	// structural. No uncommitted ledger change exists, so the reset is safe.
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"),
		[]byte("schema_version: 5\ntargets: {T1012: {status: achieved}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "commit", "-q", "--only", "bullseye.yaml", "-m", "achieve T1012")

	env := &Env{Store: &Store{Root: t.TempDir()}, RepoRoot: dir, Now: time.Now}
	got, err := env.Pre(bashPayload(dir, "git reset --hard"))
	if err != nil {
		t.Fatalf("Pre: %v", err)
	}
	if got.Verdict != Allow {
		t.Fatalf("Pre(git reset --hard) with clean ledger = %+v, want Allow", got)
	}
}

func TestGitCleanForceRefusedWhileLedgerDirty(t *testing.T) {
	dir := ledgerRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"),
		[]byte("schema_version: 5\ntargets: {T1014: {status: achieved}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	env := &Env{Store: &Store{Root: t.TempDir()}, RepoRoot: dir, Now: time.Now}
	// `git clean` alone never touches tracked files, but a path-less `-f` is
	// the same "I mean everything" shape as `reset --hard` and workers reach
	// for both in a "throw away this mess" recovery step — the guard treats
	// it identically rather than leaving a second silent-discard shape open.
	got, err := env.Pre(bashPayload(dir, "git clean -fd"))
	if err != nil {
		t.Fatalf("Pre: %v", err)
	}
	if got.Verdict != Deny {
		t.Fatalf("Pre(git clean -fd) with dirty ledger = %+v, want Deny", got)
	}
}

func TestGitCheckoutPathOfOtherFileStillAllowedWithDirtyLedger(t *testing.T) {
	// Negative control (🎯T391's own direction pin, applied here): a path
	// restore that does NOT name the ledger, and is not a whole-tree discard,
	// must not be refused just because the ledger happens to be dirty
	// elsewhere in the same working tree.
	dir := ledgerRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"),
		[]byte("schema_version: 5\ntargets: {T1015: {status: achieved}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "other.go")
	runGit(t, dir, "commit", "-q", "-m", "add other.go")
	if err := os.WriteFile(filepath.Join(dir, "other.go"), []byte("package x // dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	env := &Env{Store: &Store{Root: t.TempDir()}, RepoRoot: dir, Now: time.Now}
	got, err := env.Pre(bashPayload(dir, "git checkout -- other.go"))
	if err != nil {
		t.Fatalf("Pre: %v", err)
	}
	if got.Verdict != Allow {
		t.Fatalf("Pre(git checkout -- other.go) = %+v, want Allow (ledger dirtiness elsewhere is not this write's business)", got)
	}
}
