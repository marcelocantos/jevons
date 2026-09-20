// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 🎯T719: a gate run that executed no tests is never GREEN.

func TestT719EmptyRunClassifier(t *testing.T) {
	for _, tc := range []struct {
		name  string
		out   string
		empty bool
	}{
		{"no such -run", "ok  \texample.com/t719\t0.14s [no tests to run]\n", true},
		{"warning plus ok", "testing: warning: no tests to run\nPASS\nok  \texample.com/t719\t0.14s [no tests to run]\n", true},
		{"no test files", "?   \texample.com/t719\t[no test files]\n", true},
		{"json empty", `{"Action":"output","Package":"p","Output":"testing: warning: no tests to run\n"}` + "\n" +
			`{"Action":"output","Package":"p","Output":"ok  \tp\t0.14s [no tests to run]\n"}` + "\n" +
			`{"Action":"pass","Package":"p","Elapsed":0.14}` + "\n", true},
		{"mixed packages", "ok  \texample.com/a\t0.15s\nok  \texample.com/b\t0.01s [no tests to run]\n", false},
		{"skipped tests", "=== RUN   TestFoo\n--- SKIP: TestFoo (0.00s)\nPASS\nok  \texample.com/t719\t0.01s\n", false},
		{"json test pass", `{"Action":"pass","Package":"p","Test":"TestHello","Elapsed":0}` + "\n" +
			`{"Action":"pass","Package":"p","Elapsed":0.1}` + "\n", false},
		{"echo ok", "ok\n", false},
		{"true silent", "", false},
		{"ok line only", "ok  \texample.com/t719\t0.12s\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := EmptyRun(tc.out); got != tc.empty {
				t.Fatalf("EmptyRun = %v, want %v\nout=%q", got, tc.empty, tc.out)
			}
		})
	}
}

func TestT719NoSuchRunIsEmptyNotGreen(t *testing.T) {
	rec, _ := runIn(t, "sh", "-c",
		`printf 'testing: warning: no tests to run\nPASS\nok  \texample.com/t719\t0.14s [no tests to run]\n'; exit 0`)
	if rec.Status() != "0" {
		t.Fatalf("exit=%s, want 0 (go test's own)", rec.Status())
	}
	if rec.Verdict.IsGreen() {
		t.Fatalf("empty run attested GREEN: %s", rec.Summary())
	}
	if rec.Verdict != VerdictEmpty {
		t.Fatalf("verdict=%s, want %s", rec.Verdict, VerdictEmpty)
	}
	line := rec.Attestation()
	if strings.Contains(line, "exit=0 GREEN") {
		t.Fatalf("mutation: empty run restored a bare GREEN: %s", line)
	}
	if !strings.Contains(line, "exit=0 EMPTY") {
		t.Fatalf("GATE line missing EMPTY: %s", line)
	}
	if !strings.Contains(rec.Summary(), "no tests executed") {
		t.Fatalf("Summary does not name the empty run:\n%s", rec.Summary())
	}
}

func TestT719TrueStaysGreen(t *testing.T) {
	rec, _ := runIn(t, "true")
	if rec.Verdict != VerdictGreen && rec.Verdict != VerdictDirty {
		t.Fatalf("true = %s, want GREEN or DIRTY (tree), not EMPTY", rec.Verdict)
	}
	if rec.Verdict == VerdictEmpty {
		t.Fatal("over-broad: true classified as EMPTY")
	}
}

func TestT719GoTestNoSuchPatternIsEmpty(t *testing.T) {
	root := t719Module(t, true)
	rec, err := Run(&RunArgs{
		Command: []string{"go", "test", ".", "-count=1", "-timeout", "30s", "-run", "NoSuchTestPatternT719"},
		Dir:     root, Store: storeAt(t), Stdout: nil, Stderr: nil,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.Status() != "0" {
		t.Fatalf("go test exited %s\n%s", rec.Status(), rec.Summary())
	}
	if rec.Verdict.IsGreen() {
		t.Fatalf("go test -run NoSuch attested GREEN: %s", rec.Summary())
	}
	if rec.Verdict != VerdictEmpty {
		t.Fatalf("verdict=%s, want EMPTY\n%s", rec.Verdict, rec.Summary())
	}
}

func TestT719GoTestThatRunsATestIsNotEmpty(t *testing.T) {
	root := t719Module(t, true)
	rec, err := Run(&RunArgs{
		Command: []string{"go", "test", ".", "-count=1", "-timeout", "30s", "-run", "TestHello"},
		Dir:     root, Store: storeAt(t), Stdout: nil, Stderr: nil,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.Verdict == VerdictEmpty {
		t.Fatalf("a run that executed a test was EMPTY:\n%s", rec.Summary())
	}
	if rec.Verdict != VerdictGreen {
		t.Fatalf("clean module that ran a test = %s, want GREEN\n%s", rec.Verdict, rec.Summary())
	}
}

func TestT719NoTestFilesIsEmpty(t *testing.T) {
	root := t719Module(t, false)
	rec, err := Run(&RunArgs{
		Command: []string{"go", "test", ".", "-count=1", "-timeout", "30s"},
		Dir:     root, Store: storeAt(t), Stdout: nil, Stderr: nil,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.Status() != "0" {
		t.Fatalf("go test exited %s\n%s", rec.Status(), rec.Summary())
	}
	if rec.Verdict.IsGreen() || rec.Verdict != VerdictEmpty {
		t.Fatalf("no-test-files = %s, want EMPTY\n%s", rec.Verdict, rec.Summary())
	}
}

func TestT719EmptyOnDirtyTreeStaysEmpty(t *testing.T) {
	root := t719Module(t, true)
	writeFixture(t, root, "neighbour-wip.txt", "someone else's hunk\n")
	rec, err := Run(&RunArgs{
		Command: []string{"go", "test", ".", "-count=1", "-timeout", "30s", "-run", "NoSuchTestPatternT719"},
		Dir:     root, Store: storeAt(t), Stdout: nil, Stderr: nil,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.Verdict.IsGreen() {
		t.Fatalf("empty+dirty attested GREEN: %s", rec.Summary())
	}
	if rec.Verdict != VerdictEmpty {
		t.Fatalf("empty+dirty = %s, want EMPTY (not DIRTY)\n%s", rec.Verdict, rec.Summary())
	}
	line := rec.Attestation()
	if !strings.Contains(line, "tree=dirty+") {
		t.Fatalf("lost dirty token: %s", line)
	}
	if strings.Contains(line, "DIRTY") {
		t.Fatalf("EMPTY lost to DIRTY: %s", line)
	}
	if !strings.Contains(rec.Summary(), "tree was also dirty") {
		t.Fatalf("Summary dropped the dirt fact:\n%s", rec.Summary())
	}
}

func TestT719CitingEmptyGateIsNotGreen(t *testing.T) {
	rec, store := runIn(t, "sh", "-c",
		`printf 'ok  \texample.com/t719\t0.01s [no tests to run]\n'; exit 0`)
	report := "🎯T719 done. Commit `deadbeef`.\n\n    " + rec.Attestation() + "\n"
	flags := FlagFalseGreen(report, store.Lookup)
	found := false
	for _, f := range flags {
		if f.Kind == FlagAttestationNotGreen {
			found = true
		}
		if f.Kind == FlagDirtyTreeGate {
			t.Errorf("empty cite flagged as dirty_tree_gate: %s", f)
		}
	}
	if !found {
		t.Fatalf("citing EMPTY as a pass was not attestation_not_green; flags=%v", flags)
	}
}

func TestT719ParseAttestationsRoundTripEmpty(t *testing.T) {
	rec := &Record{
		ID: "a1b2c3d4", Name: "go-test", ExitStatus: 0, StatusKnown: true,
		Verdict: VerdictEmpty, OutputSHA256: strings.Repeat("c", 64),
		Command: []string{"go", "test", ".", "-run", "NoSuch"},
	}
	cited := ParseAttestations(rec.Attestation())
	if len(cited) != 1 {
		t.Fatalf("parsed %d attestations from %s", len(cited), rec.Attestation())
	}
	if cited[0].Verdict != VerdictEmpty || cited[0].ID != rec.ID || !cited[0].StatusIsZero() {
		t.Fatalf("round trip lost EMPTY: %+v", cited[0])
	}
}

func t719Module(t *testing.T, withTest bool) string {
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
	writeFixture(t, root, "go.mod", "module example.com/t719\n\ngo 1.22\n")
	writeFixture(t, root, "p.go", "package t719\n\nfunc Hello() string { return \"ok\" }\n")
	if withTest {
		writeFixture(t, root, "p_test.go", "package t719\n\nimport \"testing\"\n\nfunc TestHello(t *testing.T) {\n\tif Hello() != \"ok\" {\n\t\tt.Fatal(Hello())\n\t}\n}\n")
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
