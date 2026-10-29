// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 🎯T1267: `bin/gate -clean` on its own (not only through cmd/integrate's
// post-merge gate, internal/worktree/t1146_post_merge_gate.go) honors a
// repo's .jevons-integrate-prereq.yaml. Without this, a worker running the
// gate directly in a repo that declares a submodule/script prerequisite saw
// a spurious RED from missing vendor artifacts the prerequisite would have
// produced — a real specimen: pigeon's cwire/dist/pigeon.c needing
// libsodium's headers from a submodule init.

const t1267PrereqScript = `#!/bin/sh
set -e
mkdir -p generated
printf 'present\n' > generated/marker.txt
`

func t1267WriteExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func t1267WriteFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func t1267Git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// t1267Repo builds a pristine git repo. withPrereq controls whether
// .jevons-integrate-prereq.yaml is committed alongside the script.
func t1267Repo(t *testing.T, withPrereq bool) string {
	t.Helper()
	root := t.TempDir()
	t1267WriteExec(t, filepath.Join(root, "scripts/gen.sh"), t1267PrereqScript)
	added := []string{"scripts/gen.sh"}
	if withPrereq {
		t1267WriteFile(t, filepath.Join(root, ".jevons-integrate-prereq.yaml"), "script: scripts/gen.sh\n")
		added = append(added, ".jevons-integrate-prereq.yaml")
	}
	t1267Git(t, root, "init", "-q", "-b", "master")
	t1267Git(t, root, append([]string{"add"}, added...)...)
	t1267Git(t, root, "update-index", "--chmod=+x", "scripts/gen.sh")
	t1267Git(t, root, "-c", "user.email=fixture@example.com", "-c", "user.name=Fixture",
		"-c", "commit.gpgsign=false", "commit", "-q", "-m", "initial")
	return root
}

// TestGateCLICleanRunsDeclaredPrereqBeforeTheMeasuredCommand is the
// hermetic acceptance for 🎯T1267: a repo carrying the prereq file passes
// `gate -clean` against a command that depends on what the script produces,
// without the caller running the script by hand first.
func TestGateCLICleanRunsDeclaredPrereqBeforeTheMeasuredCommand(t *testing.T) {
	store := t.TempDir()
	root := t1267Repo(t, true)

	code, out := runGate(t, store, "-clean", "-dir", root, "--",
		"sh", "-c", "test -f generated/marker.txt && cat generated/marker.txt")
	if code != 0 {
		t.Fatalf("gate -clean exited %d for a repo with a satisfying prereq\n%s", code, out)
	}
	if !strings.Contains(out, "exit=0 GREEN") {
		t.Fatalf("expected a green attestation:\n%s", out)
	}
}

// TestGateCLICleanWithoutPrereqFileBehavesAsBefore is the regression half:
// a repo with no prerequisite declaration still fails exactly as it always
// did when the measured command depends on something nobody produced.
func TestGateCLICleanWithoutPrereqFileBehavesAsBefore(t *testing.T) {
	store := t.TempDir()
	root := t1267Repo(t, false)

	code, out := runGate(t, store, "-clean", "-dir", root, "--",
		"sh", "-c", "test -f generated/marker.txt")
	if code == 0 {
		t.Fatalf("expected a RED gate with no declared prereq, got exit 0\n%s", out)
	}
	if !strings.Contains(out, "RED") {
		t.Fatalf("expected a RED attestation:\n%s", out)
	}
}

// TestGateCLICleanFailingPrereqRefusesWithClearError: a declared but
// failing prerequisite refuses the gate (no record claiming a pass over a
// tree the prerequisite never actually prepared).
func TestGateCLICleanFailingPrereqRefusesWithClearError(t *testing.T) {
	store := t.TempDir()
	root := t.TempDir()
	t1267WriteExec(t, filepath.Join(root, "scripts/fail.sh"), "#!/bin/sh\necho boom >&2\nexit 1\n")
	t1267WriteFile(t, filepath.Join(root, ".jevons-integrate-prereq.yaml"), "script: scripts/fail.sh\n")
	t1267Git(t, root, "init", "-q", "-b", "master")
	t1267Git(t, root, "add", "scripts/fail.sh", ".jevons-integrate-prereq.yaml")
	t1267Git(t, root, "update-index", "--chmod=+x", "scripts/fail.sh")
	t1267Git(t, root, "-c", "user.email=fixture@example.com", "-c", "user.name=Fixture",
		"-c", "commit.gpgsign=false", "commit", "-q", "-m", "failing prereq")

	code, out := runGate(t, store, "-clean", "-dir", root, "--", "true")
	if code == 0 {
		t.Fatalf("expected a non-zero exit for a failing prerequisite\n%s", out)
	}
	if !strings.Contains(out, "prerequisite failed") {
		t.Fatalf("refusal must name the prerequisite failure:\n%s", out)
	}
	if !strings.Contains(out, "scripts/fail.sh") {
		t.Fatalf("refusal must name the script:\n%s", out)
	}
}
