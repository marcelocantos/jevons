// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitscope_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGoCompileGuardThroughTheShippedHook pins 🎯T822: refuse staged Go that
// does not compile on the index tree; allow the fix; ignore foreign worktree
// edits and non-Go commits.
func TestGoCompileGuardThroughTheShippedHook(t *testing.T) {
	r := newGuardedRepo(t)
	mod := `module example.com/t822

go 1.22
`
	goodV1 := `package main

import (
	"fmt"
	"os/exec"
)

func main() {
	_ = exec.Command("true")
	fmt.Println("v1")
}
`
	goodV2 := `package main

import (
	"fmt"
	"os/exec"
)

func main() {
	_ = exec.Command("true")
	fmt.Println("v2")
}
`
	bad := `package main

func main() {
	_ = exec.Command("true")
}
`
	if err := os.MkdirAll(filepath.Join(r.dir, "scripts", "fixture"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.write(t, "go.mod", mod)
	r.write(t, "scripts/fixture/main.go", goodV1)
	r.write(t, "notes.txt", "n1")
	r.git(t, "add", ".")
	if out, err := r.gitEnv(t, []string{"JEVONS_COMMIT_SCOPE=off"}, "commit", "-q", "-m", "seed", "--no-verify"); err != nil {
		t.Fatalf("seed: %v\n%s", err, out)
	}

	// Foreign worker's broken edit in the work tree only.
	r.write(t, "scripts/fixture/main.go", bad)

	// Non-Go commit is not refused by the compile guard.
	r.write(t, "notes.txt", "n2")
	if out, err := r.git(t, "commit", "--only", "notes.txt", "-m", "notes"); err != nil {
		t.Fatalf("non-go commit refused:\n%s", out)
	}

	before := r.head(t)
	r.write(t, "scripts/fixture/main.go", bad)
	out, err := r.git(t, "commit", "--only", "scripts/fixture/main.go", "-m", "broken go")
	if err == nil || !strings.Contains(out, "undefined: exec") || !strings.Contains(out, "🎯T822") {
		t.Fatalf("broken go was not refused with compiler line (err=%v):\n%s", err, out)
	}
	if r.head(t) != before {
		t.Fatal("HEAD moved on refused compile")
	}

	r.write(t, "scripts/fixture/main.go", goodV2)
	if out, err := r.git(t, "commit", "--only", "scripts/fixture/main.go", "-m", "fixed go"); err != nil {
		t.Fatalf("fixed go refused:\n%s", out)
	}
}

// A test-only package (scripts/docratchet's shape) has nothing for `go build`;
// the guard compiles its test binary instead. A clean one is not refused, and
// a broken _test.go still is (2026-10-22: "no non-test Go files" refused a
// clean docratchet test).
func TestGoCompileGuardHandlesTestOnlyPackages(t *testing.T) {
	r := newGuardedRepo(t)
	r.write(t, "go.mod", "module example.com/t822b\n\ngo 1.22\n")
	if err := os.MkdirAll(filepath.Join(r.dir, "scripts", "ratchet"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.write(t, "scripts/ratchet/a_test.go", "package ratchet_test\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n")
	r.git(t, "add", ".")
	if out, err := r.gitEnv(t, []string{"JEVONS_COMMIT_SCOPE=off"}, "commit", "-q", "-m", "seed", "--no-verify"); err != nil {
		t.Fatalf("seed: %v\n%s", err, out)
	}

	r.write(t, "scripts/ratchet/b_test.go", "package ratchet_test\n\nimport \"testing\"\n\nfunc TestB(t *testing.T) {}\n")
	r.git(t, "add", "scripts/ratchet/b_test.go")
	if out, err := r.git(t, "commit", "--only", "scripts/ratchet/b_test.go", "-m", "clean test-only"); err != nil {
		t.Fatalf("clean test-only package refused:\n%s", out)
	}

	r.write(t, "scripts/ratchet/c_test.go", "package ratchet_test\n\nimport \"testing\"\n\nfunc TestC(t *testing.T) { undefinedThing() }\n")
	r.git(t, "add", "scripts/ratchet/c_test.go")
	out, err := r.git(t, "commit", "--only", "scripts/ratchet/c_test.go", "-m", "broken test-only")
	if err == nil || !strings.Contains(out, "undefined: undefinedThing") {
		t.Fatalf("broken test-only package was not refused (err=%v):\n%s", err, out)
	}
}

// Two ordinary packages in one commit compile together. go build refused a
// file -o target for more than one package, so the guard blocked every
// multi-package commit (2026-10-22: internal/envelope + internal/mcpserver).
func TestGoCompileGuardHandlesMultiplePackages(t *testing.T) {
	r := newGuardedRepo(t)
	r.write(t, "go.mod", "module example.com/t822c\n\ngo 1.22\n")
	for _, d := range []string{"a", "b", "cmd/tool"} {
		if err := os.MkdirAll(filepath.Join(r.dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	r.write(t, "a/a.go", "package a\n\nfunc A() int { return 1 }\n")
	r.write(t, "b/b.go", "package b\n\nfunc B() int { return 2 }\n")
	r.write(t, "cmd/tool/main.go", "package main\n\nfunc main() {}\n")
	r.git(t, "add", ".")
	if out, err := r.gitEnv(t, []string{"JEVONS_COMMIT_SCOPE=off"}, "commit", "-q", "-m", "seed", "--no-verify"); err != nil {
		t.Fatalf("seed: %v\n%s", err, out)
	}
	r.write(t, "a/a.go", "package a\n\nfunc A() int { return 3 }\n")
	r.write(t, "b/b.go", "package b\n\nfunc B() int { return 4 }\n")
	r.write(t, "cmd/tool/main.go", "package main\n\nfunc main() { println() }\n")
	if out, err := r.git(t, "commit", "--only", "a/a.go", "b/b.go", "cmd/tool/main.go", "-m", "three packages"); err != nil {
		t.Fatalf("multi-package commit refused:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(r.dir, "tool")); err == nil {
		t.Fatal("the guard left a main binary in the work tree")
	}
	// Two library packages and no main: a directory -o target fails here.
	r.write(t, "a/a.go", "package a\n\nfunc A() int { return 5 }\n")
	r.write(t, "b/b.go", "package b\n\nfunc B() int { return 6 }\n")
	if out, err := r.git(t, "commit", "--only", "a/a.go", "b/b.go", "-m", "two libraries"); err != nil {
		t.Fatalf("two-library commit refused:\n%s", out)
	}
}

// TestT1285DeletingNestedModuleIsNotRefused pins the 2026-10-29 specimen:
// deleting a directory with its own go.mod was refused with
// "stat …/commitscope-gocompile-…/<dir>: directory not found" even though
// root `go build ./...` was green. Deleted packages are dropped; a leftover
// nested module compiles inside itself, and a broken one is still refused.
func TestT1285DeletingNestedModuleIsNotRefused(t *testing.T) {
	r := newGuardedRepo(t)
	if err := os.MkdirAll(filepath.Join(r.dir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.write(t, "go.mod", "module example.com/t1285\n\ngo 1.22\n")
	r.write(t, "lib.go", "package t1285\n")
	r.write(t, "nested/go.mod", "module example.com/nested\n\ngo 1.22\n")
	r.write(t, "nested/main.go", "package main\nfunc main() {}\n")
	r.git(t, "add", ".")
	if out, err := r.gitEnv(t, []string{"JEVONS_COMMIT_SCOPE=off"}, "commit", "-q", "-m", "seed", "--no-verify"); err != nil {
		t.Fatalf("seed: %v\n%s", err, out)
	}

	r.write(t, "nested/main.go", "package main\nfunc main() { println(\"ok\") }\n")
	if out, err := r.git(t, "commit", "--only", "nested/main.go", "-m", "nested compiles in its module"); err != nil {
		t.Fatalf("valid nested-module edit refused (compiled as root?):\n%s", out)
	}

	before := r.head(t)
	r.write(t, "nested/main.go", "package main\nfunc main() { undefinedX() }\n")
	out, err := r.git(t, "commit", "--only", "nested/main.go", "-m", "broken nested")
	if err == nil || !strings.Contains(out, "undefined: undefinedX") {
		t.Fatalf("broken nested module was not refused (err=%v):\n%s", err, out)
	}
	if r.head(t) != before {
		t.Fatal("HEAD moved on refused nested compile")
	}
	if out, err := r.git(t, "checkout", "-q", "--", "nested/main.go"); err != nil {
		t.Fatalf("restore nested/main.go: %v\n%s", err, out)
	}

	if out, err := r.git(t, "rm", "-r", "-q", "nested"); err != nil {
		t.Fatalf("git rm nested: %v\n%s", err, out)
	}
	out, err = r.git(t, "commit", "--only", "nested/go.mod", "nested/main.go", "-m", "delete nested module")
	if err != nil {
		t.Fatalf("deleting nested module refused:\n%s", out)
	}
	if strings.Contains(out, "directory not found") {
		t.Fatalf("deletion still reported the T822 stat error:\n%s", out)
	}
}
