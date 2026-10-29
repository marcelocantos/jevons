// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/worktree"
)

// 🎯T1151.7: a preserved registry row whose WorkDir still points at the shared
// clone must not survive a fresh work-seat start — isolation must land on disk.
func TestT11517FreshStartUnderPreservedRowIsolatesWorkdir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	base := filepath.Join(dir, "jevons")
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", base}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit("init", "-q")
	runGit("config", "user.email", "t@example.com")
	runGit("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(base, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit("add", ".")
	runGit("commit", "-q", "-m", "seed")

	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	const name = "jv-t966-auto"
	// Stale row: shared clone as WorkDir (the 2026-10-22 incident shape).
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: base, SessionID: "s-" + name,
		Purpose: claudia.PurposeWork, Parent: "jevons-po",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetAgent(name, fleetintent.Reaped, "product:kill", "", time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	reg.SetDirect(true)
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			if filepath.Clean(cfg.WorkDir) != filepath.Clean(worktree.WorktreePath(base, name)) {
				t.Fatalf("Launch WorkDir = %q, want isolated %q", cfg.WorkDir, worktree.WorktreePath(base, name))
			}
			return claudia.StartStub(ctx, cfg, nil)
		},
	})

	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.SetFleetIntentStore(store)
	t.Cleanup(reg.StopAll)

	if err := s.spawnFrontierWorker(name, base, "jevons-po", "T1151.7", ""); err != nil {
		t.Fatalf("spawnFrontierWorker: %v", err)
	}
	def := reg.Def(name)
	if def == nil {
		t.Fatal("no registry row after fresh start")
	}
	want := worktree.WorktreePath(base, name)
	if filepath.Clean(def.WorkDir) != filepath.Clean(want) {
		t.Fatalf("registry WorkDir = %q, want isolated worktree %q (not shared clone %q)", def.WorkDir, want, base)
	}
	if got := s.fleetIntent().AgentState(name); got != fleetintent.Working {
		t.Fatalf("intent = %q, want working", got)
	}
}
