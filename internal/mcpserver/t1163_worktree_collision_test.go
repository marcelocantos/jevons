// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/worktree"
)

// 🎯T1163: a start that passes another seat's linked worktree as workdir must
// not keep that path — each registered name gets WorktreePath(shared, name).
func TestT1163StartWithSiblingWorkdirGetsOwnTree(t *testing.T) {
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

	const seatA = "jv-t1159-auto"
	const seatB = "jv-t1155-auto"
	sibling, err := worktree.Ensure(base, seatA)
	if err != nil {
		t.Fatalf("Ensure(%s): %v", seatA, err)
	}

	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			want := worktree.WorktreePath(base, seatB)
			if filepath.Clean(cfg.WorkDir) != filepath.Clean(want) {
				t.Fatalf("Launch WorkDir = %q, want own tree %q (not sibling %q)", cfg.WorkDir, want, sibling)
			}
			return claudia.StartStub(ctx, cfg, nil)
		},
	})

	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.SetFleetIntentStore(store)
	t.Cleanup(reg.StopAll)

	// Incident shape: remint passes stale WorkDir copied from another seat.
	if err := s.spawnFrontierWorker(seatB, sibling, "jevons-po", "T1163", ""); err != nil {
		t.Fatalf("spawnFrontierWorker: %v", err)
	}
	def := reg.Def(seatB)
	if def == nil {
		t.Fatal("no registry row")
	}
	wantB := worktree.WorktreePath(base, seatB)
	if filepath.Clean(def.WorkDir) != filepath.Clean(wantB) {
		t.Fatalf("WorkDir = %q, want %q", def.WorkDir, wantB)
	}
	if filepath.Clean(def.WorkDir) == filepath.Clean(sibling) {
		t.Fatalf("seat %s still shares sibling tree %s", seatB, sibling)
	}
}

// Incident expansion (2026-10-22): five historical seat names all showed the
// same sibling path .../jv-t1159-auto — not target_id hashing; PO/remint kept
// passing that stale workdir until isolation redirected per name.
func TestT1163FiveSeatsStaleSiblingPathAllDistinct(t *testing.T) {
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

	stale, err := worktree.Ensure(base, "jv-t1159-auto")
	if err != nil {
		t.Fatal(err)
	}
	names := []string{
		"jv-t995-auto",
		"jv-t1155-auto",
		"jv-t991-auto",
		"jv-t1151.4-auto",
		"jv-t1159-auto",
	}
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			return claudia.StartStub(ctx, cfg, nil)
		},
	})
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.SetFleetIntentStore(store)
	t.Cleanup(reg.StopAll)

	seen := make(map[string]string)
	for _, name := range names {
		if err := s.spawnFrontierWorker(name, stale, "jevons-po", "T1163", ""); err != nil {
			t.Fatalf("spawn %s: %v", name, err)
		}
		def := reg.Def(name)
		if def == nil {
			t.Fatalf("no row for %s", name)
		}
		want := worktree.WorktreePath(base, name)
		if filepath.Clean(def.WorkDir) != filepath.Clean(want) {
			t.Fatalf("%s WorkDir = %q, want %q", name, def.WorkDir, want)
		}
		if other, ok := seen[def.WorkDir]; ok {
			t.Fatalf("%s and %s share WorkDir %q", name, other, def.WorkDir)
		}
		seen[def.WorkDir] = name
	}
}

// 🎯T1163 acceptance: concurrent spawns for two names never resolve to one path.
func TestT1163ConcurrentSpawnDistinctWorkdirs(t *testing.T) {
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
	reg.SetDirect(true)
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			return claudia.StartStub(ctx, cfg, nil)
		},
	})

	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.SetFleetIntentStore(store)
	t.Cleanup(reg.StopAll)

	names := []string{"jv-t1163-a-auto", "jv-t1163-b-auto"}
	var wg sync.WaitGroup
	errs := make([]error, len(names))
	for i, name := range names {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			errs[i] = s.spawnFrontierWorker(name, base, "jevons-po", "T1163", "")
		}(i, name)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("spawn %s: %v", names[i], err)
		}
	}
	paths := make([]string, len(names))
	for i, name := range names {
		def := reg.Def(name)
		if def == nil {
			t.Fatalf("no row for %s", name)
		}
		paths[i] = def.WorkDir
	}
	if paths[0] == paths[1] {
		t.Fatalf("concurrent spawns got identical WorkDir: %s", paths[0])
	}
	wantA := worktree.WorktreePath(base, names[0])
	wantB := worktree.WorktreePath(base, names[1])
	if filepath.Clean(paths[0]) != filepath.Clean(wantA) || filepath.Clean(paths[1]) != filepath.Clean(wantB) {
		t.Fatalf("paths = %q and %q, want %q and %q", paths[0], paths[1], wantA, wantB)
	}
}
