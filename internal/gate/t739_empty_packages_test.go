// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// 🎯T739: a partly-empty multi-package gate names the empty packages.

const t739Mixed = "ok  \texample.com/jevons/a\t0.15s\n" +
	"ok  \texample.com/jevons/b\t0.01s [no tests to run]\n" +
	"?   \texample.com/jevons/c\t[no test files]\n"

func TestT739EmptyPackagesParse(t *testing.T) {
	got := EmptyPackages(t739Mixed)
	want := []string{"example.com/jevons/b", "example.com/jevons/c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EmptyPackages = %v, want %v", got, want)
	}
	if got := EmptyPackages("ok\n"); got != nil {
		t.Fatalf("non-Go output yielded %v", got)
	}
}

func TestT739PartlyEmptyRunIsGreenButNamesEmptyPackage(t *testing.T) {
	rec, _ := runIn(t, "sh", "-c",
		`printf 'ok  \texample.com/a\t0.15s\nok  \texample.com/b\t0.01s [no tests to run]\n'; exit 0`)
	if rec.Verdict != VerdictGreen {
		t.Fatalf("verdict=%s, want GREEN overall", rec.Verdict)
	}
	if want := []string{"example.com/b"}; !reflect.DeepEqual(rec.EmptyPackages, want) {
		t.Fatalf("empty set = %v, want exactly %v", rec.EmptyPackages, want)
	}
	if line := rec.Attestation(); !strings.Contains(line, "exit=0 GREEN") || !strings.Contains(line, "empty=example.com/b") {
		t.Fatalf("verdict line does not name the empty package: %s", line)
	}
	if !strings.Contains(rec.Summary(), "example.com/b") {
		t.Fatalf("summary omits empty package: %s", rec.Summary())
	}
	if cited := ParseAttestations(rec.Attestation()); len(cited) != 1 || cited[0].ID != rec.ID {
		t.Fatalf("token broke attestation parsing: %+v", cited)
	}
}

func TestT739AllEmptyStillEmptyVerdict(t *testing.T) {
	rec, _ := runIn(t, "sh", "-c",
		`printf 'ok  \texample.com/a\t0.1s [no tests to run]\nok  \texample.com/b\t0.1s [no tests to run]\n'; exit 0`)
	if rec.Verdict != VerdictEmpty {
		t.Fatalf("verdict=%s, want EMPTY (🎯T719)", rec.Verdict)
	}
	if len(rec.EmptyPackages) != 2 {
		t.Fatalf("empty set = %v", rec.EmptyPackages)
	}
}

func TestT739CheckerFlagsCitationOfEmptyPackage(t *testing.T) {
	rec, store := runIn(t, "sh", "-c",
		`printf 'ok  \texample.com/jevons/internal/a\t0.15s\nok  \texample.com/jevons/internal/b\t0.01s [no tests to run]\n'; exit 0`)
	lookup := store.Lookup
	report := "Oracle for the b behaviour: internal/b is covered.\n" + rec.Attestation() + "\n"
	if !hasKind(FlagFalseGreen(report, lookup), FlagAttestationEmptyPackage) {
		t.Fatalf("citation of a green gate for an empty package was not flagged")
	}
	// Controls: honest statement, and a report about the package that did run.
	honest := "internal/b has no tests in this run.\n" + rec.Attestation() + "\n"
	if hasKind(FlagFalseGreen(honest, lookup), FlagAttestationEmptyPackage) {
		t.Fatalf("honest no-tests line flagged")
	}
	other := "Behaviour in internal/a proven.\n" + rec.Attestation() + "\n"
	if hasKind(FlagFalseGreen(other, lookup), FlagAttestationEmptyPackage) {
		t.Fatalf("report about the non-empty package flagged")
	}
}

// Acceptance hermetic: a real go test over two packages where -run matches only
// one leaves the other in EmptyPackages while the overall verdict stays GREEN.
func TestT739GoTestTwoPackagesOneEmpty(t *testing.T) {
	root := t739TwoPackageModule(t)
	rec, err := Run(&RunArgs{
		Command: []string{
			"go", "test", "./withrun", "./nomatch",
			"-count=1", "-timeout", "30s", "-run", "TestRuns",
		},
		Dir: root, Store: storeAt(t), Stdout: nil, Stderr: nil,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.Verdict != VerdictGreen {
		t.Fatalf("verdict=%s, want GREEN\n%s", rec.Verdict, rec.Summary())
	}
	want := []string{"example.com/t739/nomatch"}
	if !reflect.DeepEqual(rec.EmptyPackages, want) {
		t.Fatalf("empty set = %v, want exactly %v\n%s", rec.EmptyPackages, want, rec.Summary())
	}
	line := rec.Attestation()
	if !strings.Contains(line, "empty=example.com/t739/nomatch") {
		t.Fatalf("GATE line missing empty package token: %s", line)
	}
}

func t739TwoPackageModule(t *testing.T) string {
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
	writeFixture(t, root, "go.mod", "module example.com/t739\n\ngo 1.22\n")
	for _, sub := range []string{"withrun", "nomatch"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}
	writeFixture(t, root, "withrun/doc.go", "package withrun\n")
	writeFixture(t, root, "withrun/run_test.go", "package withrun\n\nimport \"testing\"\n\nfunc TestRuns(t *testing.T) {}\n")
	writeFixture(t, root, "nomatch/doc.go", "package nomatch\n")
	writeFixture(t, root, "nomatch/other_test.go", "package nomatch\n\nimport \"testing\"\n\nfunc TestOtherOnly(t *testing.T) {}\n")
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
