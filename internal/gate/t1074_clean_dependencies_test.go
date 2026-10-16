// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T1074.1 replaces the T838/T894/T918 sibling-injection policy: a clean
// gate must compile the dependency declared in the commit, even when the
// shared clone's sibling contains contradictory uncommitted implementation.
// These are real Go builds through RunClean, not checks of workspace text.
// 🎯T440 //worktreereap:exempt — every fixture worktree and its owning repo
// live inside t.TempDir, outside the shared clone's worktree registry.
func TestT1074CleanGateUsesCommittedDependency(t *testing.T) {
	for _, tc := range []struct {
		name, module, dependency string
		linked, sibling          bool
	}{
		{"primary", "jevons", "claudia", false, true},
		{"linked", "jevons", "claudia", true, true},
		{"claudia-with-vellum", "claudia", "vellum", false, true},
		{"no-sibling-control", "jevons", "claudia", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Exercise automatic workspace discovery despite the enclosing
			// suite deliberately running with GOWORK=off.
			t.Setenv("GOWORK", "")
			t.Setenv("GOFLAGS", "")
			t.Setenv("GOPROXY", "off")
			root := cleanDependencyFixture(t, tc.module, tc.dependency, tc.sibling)
			if tc.linked {
				linked := filepath.Join(filepath.Dir(root), "workers", "one")
				fixtureGit(t, root, "worktree", "add", "--detach", linked)
				root = linked
			}
			assertCleanDependency(t, root)
		})
	}
}

func TestT1074CleanGateIgnoresInheritedWorkspace(t *testing.T) {
	t.Setenv("GOFLAGS", "")
	t.Setenv("GOPROXY", "off")
	root := cleanDependencyFixture(t, "jevons", "claudia", true)
	workspace := filepath.Join(filepath.Dir(root), "go.work")
	body := fmt.Sprintf("go 1.26.1\nuse %q\nreplace github.com/marcelocantos/claudia => %q\n",
		root, filepath.Join(filepath.Dir(root), "claudia"))
	if err := os.WriteFile(workspace, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOWORK", workspace)
	// The ordinary development gate still inherits explicit workspaces.
	var ordinaryOutput bytes.Buffer
	ordinary, err := Run(&RunArgs{
		Dir: root, Command: []string{"go", "env", "GOWORK"}, Stdout: &ordinaryOutput,
	})
	if err != nil || ordinary.ExitStatus != 0 || strings.TrimSpace(ordinaryOutput.String()) != workspace {
		t.Fatalf("ordinary gate lost the explicit workspace: record=%+v err=%v output=%s", ordinary, err, &ordinaryOutput)
	}
	assertCleanDependency(t, root)
	if got := os.Getenv("GOWORK"); got != workspace {
		t.Fatalf("RunClean changed the caller's workspace: got %q, want %q", got, workspace)
	}
}

func cleanDependencyFixture(t *testing.T, module, dependency string, sibling bool) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), module)
	depDir := filepath.Join(root, "dep")
	if err := os.MkdirAll(depDir, 0o755); err != nil {
		t.Fatal(err)
	}
	depModule := "github.com/marcelocantos/" + dependency
	writeFixture(t, root, "go.mod", "module github.com/marcelocantos/"+module+"\n\ngo 1.26.1\n\nrequire "+depModule+" v0.0.0\nreplace "+depModule+" => ./dep\n")
	writeFixture(t, depDir, "go.mod", "module "+depModule+"\n\ngo 1.26.1\n")
	writeFixture(t, depDir, "value.go", "package dep\nconst Value = \"committed\"\n")
	writeFixture(t, root, "dependency_test.go", fmt.Sprintf(`package fixture
import (
    "os"
    "testing"
    dep %q
)
func TestDeclaredDependency(t *testing.T) {
    if dep.Value != "committed" {
        t.Fatalf("built mutable sibling: %%s", dep.Value)
    }
    if _, err := os.Stat("go.work"); !os.IsNotExist(err) {
        t.Fatalf("clean gate injected a workspace: %%v", err)
    }
}
`, depModule))
	fixtureGit(t, root, "init", "-q", "-b", "master")
	fixtureGit(t, root, "add", "go.mod", "dep", "dependency_test.go")
	fixtureGit(t, root, "-c", "user.email=fixture@example.com", "-c", "user.name=Fixture",
		"-c", "commit.gpgsign=false", "commit", "-q", "-m", "committed dependency")
	if sibling {
		sibDir := filepath.Join(filepath.Dir(root), dependency)
		if err := os.MkdirAll(sibDir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeFixture(t, sibDir, "go.mod", "module "+depModule+"\n\ngo 1.26.1\n")
		writeFixture(t, sibDir, "value.go", "package dep\nconst Value = \"mutable\"\n")
	}
	return root
}

func assertCleanDependency(t *testing.T, root string) {
	t.Helper()
	var output, errors bytes.Buffer
	result, err := RunClean(&CleanArgs{
		Repo: root, WorkDir: t.TempDir(), Store: storeAt(t),
		Command: []string{"go", "test", "-count=1", "-v", "./..."},
		Stdout:  &output, Stderr: &errors,
	})
	if err != nil {
		t.Fatalf("RunClean: %v\n%s\n%s", err, &output, &errors)
	}
	if result.Record.Verdict != VerdictGreen {
		t.Fatalf("declared dependency did not pass: %s\n%s\n%s", result.Record.Summary(), &output, &errors)
	}
	if !strings.Contains(output.String(), "--- PASS: TestDeclaredDependency") {
		t.Fatalf("dependency test did not execute:\n%s", &output)
	}
	if tree := result.Record.Tree; tree == nil || !tree.Clean || tree.Commit != strings.TrimSpace(fixtureGit(t, root, "rev-parse", "HEAD")) {
		t.Fatalf("wrong provenance: %+v", tree)
	}
}
