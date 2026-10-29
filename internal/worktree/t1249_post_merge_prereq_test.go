// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/worktree"
)

// 🎯T1249: a declared prerequisite that materialises a file go build needs
// lets the post-merge gate pass; missing declaration or a failing script
// still refuses.

const t1249GenScript = `#!/bin/sh
set -e
mkdir -p generated
printf 'package generated\n' > generated/generated.go
`

const t1249NeedsGenerated = `package p1

import _ "example.com/integratetest/generated"

func P() {}
`

func t1249WriteExec(t *testing.T, path, body string) {
	t.Helper()
	writeFile(t, path, body)
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestIntegratePostMergePrereqCreatesFileGoBuildNeeds(t *testing.T) {
	base := goModuleSharedClone(t)
	wt, err := worktree.Ensure(base, "jv-prereq-ok")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	t1249WriteExec(t, filepath.Join(wt, "scripts/gen.sh"), t1249GenScript)
	writeFile(t, filepath.Join(wt, "p1/p1.go"), t1249NeedsGenerated)
	writeFile(t, filepath.Join(wt, "p1/p1_test.go"), "package p1\n\nimport \"testing\"\n\nfunc TestP(t *testing.T) { P() }\n")
	writeFile(t, filepath.Join(wt, worktree.IntegratePrereqFile), "script: scripts/gen.sh\n")
	git(t, wt, "add", "scripts/gen.sh", "p1/p1.go", "p1/p1_test.go", worktree.IntegratePrereqFile)
	git(t, wt, "update-index", "--chmod=+x", "scripts/gen.sh")
	git(t, wt, "commit", "-q", "-m", "worker: generated import + prereq")

	writeFile(t, filepath.Join(base, "other.txt"), "master moved\n")
	git(t, base, "add", "other.txt")
	git(t, base, "commit", "-q", "-m", "master moves elsewhere")

	res, err := worktree.Integrate(&worktree.IntegrateArgs{
		BaseWorkdir: base, AgentName: "jv-prereq-ok", GateStoreRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Integrate: %v", err)
	}
	if res.Merge == "" {
		t.Fatalf("want merge commit, got %+v", res)
	}
	if res.PostMergeGate == nil || !res.PostMergeGate.Verdict.IsGreen() {
		t.Fatalf("want GREEN post-merge gate, got %+v", res.PostMergeGate)
	}
}

func TestIntegratePostMergeWithoutPrereqStillRefusesMissingGenerated(t *testing.T) {
	base := goModuleSharedClone(t)
	wt, err := worktree.Ensure(base, "jv-prereq-missing")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	writeFile(t, filepath.Join(wt, "p1/p1.go"), t1249NeedsGenerated)
	git(t, wt, "add", "p1/p1.go")
	git(t, wt, "commit", "-q", "-m", "worker: generated import, no prereq")

	writeFile(t, filepath.Join(base, "other.txt"), "master moved\n")
	git(t, base, "add", "other.txt")
	git(t, base, "commit", "-q", "-m", "master moves elsewhere")
	masterTip := git(t, base, "rev-parse", "master")

	_, err = worktree.Integrate(&worktree.IntegrateArgs{
		BaseWorkdir: base, AgentName: "jv-prereq-missing", GateStoreRoot: t.TempDir(),
	})
	if !errors.Is(err, worktree.ErrPostMergeGate) {
		t.Fatalf("want ErrPostMergeGate, got %v", err)
	}
	if strings.Contains(err.Error(), "prerequisite failed") {
		t.Fatalf("missing declaration must fail as a build error, not a prerequisite error: %v", err)
	}
	if now := git(t, base, "rev-parse", "master"); now != masterTip {
		t.Fatalf("refused landing left master at %s, want %s", now, masterTip)
	}
}

func TestIntegratePostMergeFailingPrereqRefusesWithClearError(t *testing.T) {
	base := goModuleSharedClone(t)
	wt, err := worktree.Ensure(base, "jv-prereq-fail")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	t1249WriteExec(t, filepath.Join(wt, "scripts/fail.sh"), "#!/bin/sh\necho boom >&2\nexit 1\n")
	writeFile(t, filepath.Join(wt, worktree.IntegratePrereqFile), "script: scripts/fail.sh\n")
	git(t, wt, "add", "scripts/fail.sh", worktree.IntegratePrereqFile)
	git(t, wt, "update-index", "--chmod=+x", "scripts/fail.sh")
	git(t, wt, "commit", "-q", "-m", "worker: failing prereq")

	writeFile(t, filepath.Join(base, "other.txt"), "master moved\n")
	git(t, base, "add", "other.txt")
	git(t, base, "commit", "-q", "-m", "master moves elsewhere")
	masterTip := git(t, base, "rev-parse", "master")

	_, err = worktree.Integrate(&worktree.IntegrateArgs{
		BaseWorkdir: base, AgentName: "jv-prereq-fail", GateStoreRoot: t.TempDir(),
	})
	if !errors.Is(err, worktree.ErrPostMergeGate) {
		t.Fatalf("want ErrPostMergeGate, got %v", err)
	}
	if !errors.Is(err, worktree.ErrIntegratePrereq) {
		t.Fatalf("want ErrIntegratePrereq, got %v", err)
	}
	if !strings.Contains(err.Error(), "prerequisite failed") {
		t.Fatalf("refusal must say prerequisite failed, got %v", err)
	}
	if !strings.Contains(err.Error(), "scripts/fail.sh") {
		t.Fatalf("refusal must name the script, got %v", err)
	}
	if now := git(t, base, "rev-parse", "master"); now != masterTip {
		t.Fatalf("refused landing left master at %s, want %s", now, masterTip)
	}
}

func TestIntegratePostMergeUnsafePrereqPathRefuses(t *testing.T) {
	base := goModuleSharedClone(t)
	wt, err := worktree.Ensure(base, "jv-prereq-escape")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	writeFile(t, filepath.Join(wt, worktree.IntegratePrereqFile), "script: ../escape.sh\n")
	git(t, wt, "add", worktree.IntegratePrereqFile)
	git(t, wt, "commit", "-q", "-m", "worker: escaping prereq")

	writeFile(t, filepath.Join(base, "other.txt"), "master moved\n")
	git(t, base, "add", "other.txt")
	git(t, base, "commit", "-q", "-m", "master moves elsewhere")
	masterTip := git(t, base, "rev-parse", "master")

	_, err = worktree.Integrate(&worktree.IntegrateArgs{
		BaseWorkdir: base, AgentName: "jv-prereq-escape", GateStoreRoot: t.TempDir(),
	})
	if !errors.Is(err, worktree.ErrPostMergeGate) || !errors.Is(err, worktree.ErrIntegratePrereq) {
		t.Fatalf("want post-merge + prereq refusal, got %v", err)
	}
	if !strings.Contains(err.Error(), "escapes the repo") {
		t.Fatalf("want path-escape error, got %v", err)
	}
	if now := git(t, base, "rev-parse", "master"); now != masterTip {
		t.Fatalf("refused landing left master at %s, want %s", now, masterTip)
	}
}
