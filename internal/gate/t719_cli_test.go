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

// 🎯T719 through the shipped binary: an empty go test -run is EMPTY, not
// GREEN; cmdRun exits 3 (cite-as-pass / &&-chain stop); last/show are
// non-zero. The GATE line still carries exit=0 (go test's own status).

func TestT719GateCLIEmptyRunIsNotGreen(t *testing.T) {
	root := t719CLIModule(t, true)
	store := t.TempDir()

	code, out := runGate(t, store, "-dir", root, "--",
		"go", "test", ".", "-count=1", "-timeout", "30s", "-run", "NoSuchTestPatternT719")
	if code == 0 {
		t.Fatalf("empty run exited 0; want non-zero so && chains stop\n%s", out)
	}
	if !strings.Contains(out, "exit=0 EMPTY") {
		t.Fatalf("GATE line missing EMPTY:\n%s", out)
	}
	if strings.Contains(out, "exit=0 GREEN") {
		t.Fatalf("mutation: empty CLI run restored a bare GREEN:\n%s", out)
	}

	lastCode, lastOut := runGate(t, store, "last")
	if lastCode == 0 {
		t.Fatalf("gate last treated EMPTY as a pass:\n%s", lastOut)
	}
	if !strings.Contains(lastOut, "EMPTY") {
		t.Fatalf("gate last lost EMPTY:\n%s", lastOut)
	}
}

func TestT719GateCLIExecutedTestStaysGreen(t *testing.T) {
	root := t719CLIModule(t, true)
	store := t.TempDir()

	code, out := runGate(t, store, "-dir", root, "--",
		"go", "test", ".", "-count=1", "-timeout", "30s", "-run", "TestHello")
	if code != 0 {
		t.Fatalf("executed test exited %d\n%s", code, out)
	}
	if !strings.Contains(out, "exit=0 GREEN") {
		t.Fatalf("executed test GATE line missing GREEN:\n%s", out)
	}
	if strings.Contains(out, "EMPTY") {
		t.Fatalf("executed test marked EMPTY:\n%s", out)
	}
}

func t719CLIModule(t *testing.T, withTest bool) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := filepath.Join(t.TempDir(), "mod")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("go.mod", "module example.com/t719\n\ngo 1.22\n")
	write("p.go", "package t719\n\nfunc Hello() string { return \"ok\" }\n")
	if withTest {
		write("p_test.go", "package t719\n\nimport \"testing\"\n\nfunc TestHello(t *testing.T) {}\n")
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q")
	run("add", ".")
	run("-c", "user.email=fixture@example.com", "-c", "user.name=Fixture",
		"-c", "commit.gpgsign=false", "commit", "-q", "-m", "mod")
	return root
}
