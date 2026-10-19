package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 🎯T805: a clean worktree has no untracked bin/jevonsd. The suite used to
// fall back to whatever jevonsd is on PATH — the released Homebrew build,
// which serves the vanilla page — so a clean-revision journey measured the
// wrong product. With no bin/jevonsd the suite builds the tree's own daemon.
func TestT805MissingBinBuildsTreeDaemonNotPATH(t *testing.T) {
	root := t.TempDir()
	built := ""
	got, err := resolveDaemon(root, "", func(dir, out string) error {
		built = dir
		return os.WriteFile(out, []byte("#!/bin/sh\n"), 0o755)
	})
	if err != nil {
		t.Fatal(err)
	}
	if built != root {
		t.Fatalf("built %q, want the tree %q", built, root)
	}
	if filepath.Base(got) != "jevonsd" || filepath.Dir(got) == "/opt/homebrew/bin" {
		t.Fatalf("resolved %q: not the tree build", got)
	}
}

func TestT805FlagBinWinsWithoutBuilding(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "given")
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	noBuild := func(string, string) error { t.Fatal("built although -bin was given"); return nil }
	if got, err := resolveDaemon(root, bin, noBuild); err != nil || got != bin {
		t.Fatalf("-bin: got %q, %v", got, err)
	}
}

func TestT805TreeBinaryIsKept(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin", "jevonsd")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := resolveDaemon(root, "", func(string, string) error {
		t.Fatal("rebuilt a tree binary that is already there")
		return nil
	})
	if err != nil || got != bin {
		t.Fatalf("tree bin: got %q, %v", got, err)
	}
}

func TestDaemonBuildCommandBuildsTreeDaemon(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(t.TempDir(), "jevonsd")
	cmd := daemonBuildCommand(dir, out)
	got := strings.Join(cmd.Args, " ")
	if got != "go build -o "+out+" ./cmd/jevonsd" || cmd.Dir != dir {
		t.Fatalf("build command %q in %q", cmd.Args, cmd.Dir)
	}
}
