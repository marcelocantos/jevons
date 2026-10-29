// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package treeguard

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/commitattrib"
	"github.com/marcelocantos/jevons/internal/worktree"
)

func TestWorkSeatRefusesEditUnderSharedClone(t *testing.T) {
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
	if err := exec.Command("mkdir", "-p", base).Run(); err != nil {
		t.Fatal(err)
	}
	run("init", "-q")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(base, "seed.txt"), []byte("s\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "seed")
	wt, err := worktree.Ensure(base, "jv-t1151-seat")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(commitattrib.ActorEnv, "jv-t1151-seat")
	target := filepath.Join(base, "owned.txt")
	body := "nope"
	p := &Payload{SessionID: "s1", ToolName: "Write"}
	p.ToolInput.FilePath = target
	p.ToolInput.Content = &body
	env := &Env{Store: &Store{Root: t.TempDir()}, RepoRoot: wt, Now: time.Now}
	d, err := env.Pre(p)
	if err != nil {
		t.Fatal(err)
	}
	if d.Verdict != Deny {
		t.Fatalf("want deny editing shared clone, got %+v", d)
	}
}
