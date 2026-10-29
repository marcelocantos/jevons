// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package treeguard

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	t1247Old  = "# content_hash: sha256:aaaa\ntargets:\n  T1244:\n    acceptance:\n      - old widget copy\n"
	t1247New  = "# content_hash: sha256:bbbb\ntargets:\n  T1244:\n    acceptance:\n      - widget draws vertical bars\n"
	t1247Drop = "# content_hash: sha256:cccc\ntargets: {}\n"
)

// 🎯T1247: a bullseye apply that replaces ledger text is journalled as a
// guarded write, so the sweep reports no UNGUARDED CHANGE.
func TestT1247SweepIgnoresJournaledBullseyeReplace(t *testing.T) {
	env, ledger := t1247Env(t)
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	if err := os.WriteFile(ledger, []byte(t1247Old), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := env.Sweep(now); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ledger, []byte(t1247New), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := recordLedgerWrite(env.Store, ledger, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	got, err := env.Sweep(now.Add(2 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("journaled bullseye replace reported as a loss: %+v", got)
	}
}

// 🎯T1247: a raw out-of-band edit that drops ledger content is still reported.
func TestT1247SweepReportsOutOfBandDrop(t *testing.T) {
	env, ledger := t1247Env(t)
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	if err := os.WriteFile(ledger, []byte(t1247Old), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := env.Sweep(now); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ledger, []byte(t1247Drop), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := env.Sweep(now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("out-of-band drop not reported: %+v", got)
	}
	joined := strings.Join(got[0].Lost, "\n")
	if !strings.Contains(joined, "old widget copy") && !strings.Contains(joined, "T1244:") {
		t.Fatalf("finding does not name the dropped ledger text: %+v", got)
	}
}

// 🎯T1247: MCP/CLI commit-mode never hits PreToolUse (commit-tree + update-ref).
// Sweep recognises a newer bullseye: commit whose HEAD blob matches disk.
func TestT1247SweepRecognizesBullseyeCommit(t *testing.T) {
	repo := t1247GitRepo(t)
	ledger := filepath.Join(repo, "bullseye.yaml")
	store := &Store{Root: t.TempDir()}
	env := &Env{Store: store, RepoRoot: repo, Guarded: []string{"bullseye.yaml"}}
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

	if err := os.WriteFile(ledger, []byte(t1247Old), 0o644); err != nil {
		t.Fatal(err)
	}
	t1247Git(t, repo, "add", "bullseye.yaml")
	t1247GitAt(t, repo, now.Add(-time.Hour), "commit", "-m", "init")
	if _, err := env.Sweep(now); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(ledger, []byte(t1247New), 0o644); err != nil {
		t.Fatal(err)
	}
	t1247Git(t, repo, "add", "bullseye.yaml")
	t1247GitAt(t, repo, now.Add(30*time.Second), "commit", "-m", "bullseye: T1244 widget draws vertical bars (owner)")

	got, err := env.Sweep(now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("bullseye commit-mode replace reported as a loss: %+v", got)
	}
}

// A checkout of HEAD that drops uncommitted rows still alarms: the last
// bullseye commit is older than the journal of those rows.
func TestT1247SweepReportsWipeToOlderBullseyeHEAD(t *testing.T) {
	repo := t1247GitRepo(t)
	ledger := filepath.Join(repo, "bullseye.yaml")
	store := &Store{Root: t.TempDir()}
	env := &Env{Store: store, RepoRoot: repo, Guarded: []string{"bullseye.yaml"}}
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

	if err := os.WriteFile(ledger, []byte(t1247Old), 0o644); err != nil {
		t.Fatal(err)
	}
	t1247Git(t, repo, "add", "bullseye.yaml")
	t1247GitAt(t, repo, now, "commit", "-m", "bullseye: ledger mutation")
	if _, err := env.Sweep(now); err != nil {
		t.Fatal(err)
	}

	uncommitted := "# content_hash: sha256:dddd\ntargets:\n  T1244:\n    acceptance:\n      - old widget copy\n  T1247:\n    name: extra uncommitted row\n"
	if err := os.WriteFile(ledger, []byte(uncommitted), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := env.Store.Journal(ledger, []byte(uncommitted), now.Add(time.Minute), "", ViaSweep); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ledger, []byte(t1247Old), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := env.Sweep(now.Add(2 * time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("wipe of uncommitted row not reported: %+v", got)
	}
	if !strings.Contains(strings.Join(got[0].Lost, "\n"), "extra uncommitted row") {
		t.Fatalf("finding does not name the wiped row: %+v", got)
	}
}

func t1247Env(t *testing.T) (*Env, string) {
	t.Helper()
	repo := t.TempDir()
	store := &Store{Root: t.TempDir()}
	env := &Env{Store: store, RepoRoot: repo, Guarded: []string{"bullseye.yaml"}}
	return env, filepath.Join(repo, "bullseye.yaml")
}

func t1247GitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	t1247Git(t, repo, "init", "-b", "master")
	t1247Git(t, repo, "config", "user.email", "treeguard@test")
	t1247Git(t, repo, "config", "user.name", "treeguard")
	t1247Git(t, repo, "config", "commit.gpgsign", "false")
	return repo
}

func t1247Git(t *testing.T, dir string, args ...string) {
	t.Helper()
	t1247GitAt(t, dir, time.Time{}, args...)
}

func t1247GitAt(t *testing.T, dir string, when time.Time, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=treeguard",
		"GIT_AUTHOR_EMAIL=treeguard@test",
		"GIT_COMMITTER_NAME=treeguard",
		"GIT_COMMITTER_EMAIL=treeguard@test",
	)
	if !when.IsZero() {
		stamp := when.Format(time.RFC3339)
		env = append(env, "GIT_AUTHOR_DATE="+stamp, "GIT_COMMITTER_DATE="+stamp)
	}
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
