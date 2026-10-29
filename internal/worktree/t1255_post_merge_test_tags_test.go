// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/worktree"
)

// 🎯T1255: a build-tagged test package is actually tested when the repo
// declares test_tags; without that declaration the post-merge gate still
// reports today's RED/no-packages outcome. Not a bypass.

const t1255TaggedPkg = `//go:build csdke2e

package csdke2e

func Relay() string { return "peer" }
`

const t1255TaggedTest = `//go:build csdke2e

package csdke2e

import "testing"

func TestRelay(t *testing.T) {
	if got := Relay(); got != "peer" {
		t.Fatalf("got %q", got)
	}
}
`

func t1255CommitTaggedPackage(t *testing.T, wt string, declareTags bool) {
	t.Helper()
	writeFile(t, filepath.Join(wt, "c/test/csdke2e/e2e.go"), t1255TaggedPkg)
	writeFile(t, filepath.Join(wt, "c/test/csdke2e/e2e_test.go"), t1255TaggedTest)
	adds := []string{"c/test/csdke2e/e2e.go", "c/test/csdke2e/e2e_test.go"}
	if declareTags {
		writeFile(t, filepath.Join(wt, worktree.IntegratePrereqFile), "test_tags: [csdke2e]\n")
		adds = append(adds, worktree.IntegratePrereqFile)
	}
	git(t, wt, append([]string{"add"}, adds...)...)
	msg := "worker: tagged csdke2e tests"
	if declareTags {
		msg += " + test_tags"
	}
	git(t, wt, "commit", "-q", "-m", msg)
}

func t1255MoveMaster(t *testing.T, base string) {
	t.Helper()
	writeFile(t, filepath.Join(base, "other.txt"), "master moved\n")
	git(t, base, "add", "other.txt")
	git(t, base, "commit", "-q", "-m", "master moves elsewhere")
}

func TestIntegratePostMergeTestTagsRunsBuildTaggedTests(t *testing.T) {
	base := goModuleSharedClone(t)
	wt, err := worktree.Ensure(base, "jv-tags-ok")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	t1255CommitTaggedPackage(t, wt, true)
	t1255MoveMaster(t, base)

	res, err := worktree.Integrate(&worktree.IntegrateArgs{
		BaseWorkdir: base, AgentName: "jv-tags-ok", GateStoreRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Integrate: %v", err)
	}
	if res.Merge == "" {
		t.Fatalf("want merge commit, got %+v", res)
	}
	rec := res.PostMergeGate
	if rec == nil || !rec.Verdict.IsGreen() {
		t.Fatalf("want GREEN post-merge gate, got %+v", rec)
	}
	cmd := strings.Join(rec.Command, " ")
	if !strings.Contains(cmd, "-tags csdke2e") {
		t.Fatalf("touched-package gate must pass -tags csdke2e, got %q", cmd)
	}
	if !strings.Contains(rec.OutputTail, "TestRelay") {
		t.Fatalf("real tagged test did not run; output:\n%s", rec.OutputTail)
	}
}

func TestIntegratePostMergeWithoutTestTagsStillNoPackages(t *testing.T) {
	base := goModuleSharedClone(t)
	wt, err := worktree.Ensure(base, "jv-tags-missing")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	t1255CommitTaggedPackage(t, wt, false)
	t1255MoveMaster(t, base)
	masterTip := git(t, base, "rev-parse", "master")

	_, err = worktree.Integrate(&worktree.IntegrateArgs{
		BaseWorkdir: base, AgentName: "jv-tags-missing", GateStoreRoot: t.TempDir(),
	})
	if !errors.Is(err, worktree.ErrPostMergeGate) {
		t.Fatalf("want ErrPostMergeGate, got %v", err)
	}
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	if !strings.Contains(msg, "matched no packages") && !strings.Contains(msg, "RED") && !strings.Contains(msg, "EMPTY") {
		t.Fatalf("want RED/no-packages outcome, got %v", err)
	}
	if strings.Contains(msg, "-tags") {
		t.Fatalf("undeclared tags must not be injected: %v", err)
	}
	if now := git(t, base, "rev-parse", "master"); now != masterTip {
		t.Fatalf("refused landing left master at %s, want %s", now, masterTip)
	}
}
