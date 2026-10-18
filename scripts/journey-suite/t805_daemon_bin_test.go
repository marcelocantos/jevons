package main

import (
	"os"
	"os/exec"
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

func TestT805UntaggedTreeBinaryIsRebuilt(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin", "jevonsd")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("not a go binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	replaced := ""
	got, err := resolveDaemon(root, "", func(dir, out string) error {
		if dir != root {
			t.Fatalf("built %q, want the tree %q", dir, root)
		}
		replaced = out
		return os.WriteFile(out, []byte("#!/bin/sh\n"), 0o755)
	})
	if err != nil {
		t.Fatal(err)
	}
	if replaced != bin || got != bin {
		t.Fatalf("replaced %q resolved %q, want the stale tree binary rebuilt", replaced, got)
	}
}

func TestT805TaggedTreeBinaryIsKept(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin", "jevonsd")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	buildTaggedHelper(t, bin)
	if !daemonCarriesControlPlane(bin) {
		t.Fatal("helper binary has no jevons_claudia_next build tag")
	}
	got, err := resolveDaemon(root, "", func(string, string) error {
		t.Fatal("rebuilt a binary that already carries the control plane")
		return nil
	})
	if err != nil || got != bin {
		t.Fatalf("tagged bin: got %q, %v", got, err)
	}
}

func TestDaemonBuildCommandCarriesControlPlaneTag(t *testing.T) {
	cmd := daemonBuildCommand(t.TempDir(), filepath.Join(t.TempDir(), "jevonsd"))
	got := strings.Join(cmd.Args, " ")
	if !strings.Contains(got, "-tags jevons_claudia_next") || !strings.Contains(got, "./cmd/jevonsd") {
		t.Fatalf("build command %q", cmd.Args)
	}
}

func buildTaggedHelper(t *testing.T, out string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/tagged\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-tags", "jevons_claudia_next", "-o", out, ".")
	cmd.Dir = dir
	env := make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "GOWORK=") {
			continue
		}
		env = append(env, entry)
	}
	cmd.Env = append(env, "GOWORK=off")
	if body, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tagged helper: %v\n%s", err, body)
	}
}
