// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitscope_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestGoCompileGuardRefusesMissingImportThroughTheShippedHook is the 🎯T822
// incident shape: a scripts/ package uses exec.Command without importing
// os/exec. The commit is refused with the compiler line; the same commit
// with the import lands; a non-Go commit is not slowed; another worker's
// dirty worktree edit neither triggers the check nor is swept in.
func TestGoCompileGuardRefusesMissingImportThroughTheShippedHook(t *testing.T) {
	r := newGuardedRepo(t)

	// Mini module so `go build` has something to resolve. The seed commit
	// is opt-out of the shared-index guard; compile has nothing to check yet.
	r.write(t, "go.mod", "module example.com/t822\n\ngo 1.22\n")
	if err := os.MkdirAll(filepath.Join(r.dir, "scripts", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	good := "package main\n\nimport \"os/exec\"\n\nfunc main() { _ = exec.Command(\"true\") }\n"
	r.write(t, "scripts/pkg/main.go", good)
	r.write(t, "notes.txt", "n1")
	if out, err := r.gitEnv(t, []string{"JEVONS_COMMIT_SCOPE=off"}, "add", "."); err != nil {
		t.Fatalf("seed add: %v\n%s", err, out)
	}
	if out, err := r.gitEnv(t, []string{"JEVONS_COMMIT_SCOPE=off"}, "commit", "-m", "seed module"); err != nil {
		t.Fatalf("seed commit: %v\n%s", err, out)
	}

	// Another worker's uncommitted broken edit sits beside the package.
	// It must not poison a non-Go commit or a fixed Go commit.
	r.write(t, "scripts/pkg/foreign.go", "package main\n\nfunc foreign() { _ = exec.Command(\"x\") }\n")

	// Non-Go commit is not slowed / not refused by dirty foreign Go.
	r.write(t, "notes.txt", "n2")
	start := time.Now()
	if out, err := r.git(t, "commit", "--only", "notes.txt", "-m", "notes"); err != nil {
		t.Fatalf("non-go commit refused by foreign dirty Go:\n%s", out)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("non-go commit took %s — compile guard should no-op", d)
	}

	// Broken staged change: missing import. Refused with compiler line.
	broken := "package main\n\nfunc main() { _ = exec.Command(\"true\") }\n"
	r.write(t, "scripts/pkg/main.go", broken)
	// Drop the foreign dirty file so the only failure is the staged missing
	// import — the foreign file is a separate control below.
	if err := os.Remove(filepath.Join(r.dir, "scripts", "pkg", "foreign.go")); err != nil {
		t.Fatal(err)
	}
	before := r.head(t)
	out, err := r.git(t, "commit", "--only", "scripts/pkg/main.go", "-m", "break import")
	if err == nil {
		t.Fatalf("missing-import commit was allowed:\n%s", r.showStat(t, "HEAD"))
	}
	if !strings.Contains(out, "🎯T822") {
		t.Errorf("refusal not attributable to T822:\n%s", out)
	}
	if !strings.Contains(out, "undefined: exec") && !strings.Contains(out, "undefined:exec") {
		t.Errorf("refusal does not name the compiler error:\n%s", out)
	}
	if !strings.Contains(out, "main.go") {
		t.Errorf("refusal does not name the file:\n%s", out)
	}
	if r.head(t) != before {
		t.Fatal("HEAD moved on a refused compile commit")
	}

	// Fixed import lands.
	r.write(t, "scripts/pkg/main.go", good)
	if out, err := r.git(t, "commit", "--only", "scripts/pkg/main.go", "-m", "fix import"); err != nil {
		t.Fatalf("fixed import refused:\n%s", out)
	}

	// Foreign dirty broken file must not refuse a commit of a still-good
	// package file (judged on the index, not the worktree).
	r.write(t, "scripts/pkg/foreign.go", "package main\n\nfunc foreign() { _ = exec.Command(\"x\") }\n")
	r.write(t, "scripts/pkg/main.go", good+"\n// touch\n")
	if out, err := r.git(t, "commit", "--only", "scripts/pkg/main.go", "-m", "touch with foreign dirt"); err != nil {
		t.Fatalf("good commit refused by foreign dirty worktree Go:\n%s", out)
	}
	if strings.Contains(r.showStat(t, "HEAD"), "foreign.go") {
		t.Fatal("foreign worktree edit leaked into the commit")
	}

	// Bypass works.
	r.write(t, "scripts/pkg/main.go", broken)
	if out, err := r.gitEnv(t, []string{"JEVONS_GO_COMPILE=off"}, "commit", "--only", "scripts/pkg/main.go", "-m", "bypass"); err != nil {
		t.Fatalf("compile bypass refused:\n%s", out)
	}
}
