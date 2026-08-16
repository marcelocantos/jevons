// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func feed(t *testing.T, lines ...string) *result {
	t.Helper()
	f, err := os.Create(filepath.Join(t.TempDir(), "t.log"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	r := &result{Packages: map[string]bool{}, transcript: f}
	for _, l := range lines {
		r.consume([]byte(l))
	}
	return r
}

func pass(pkg, test string) string {
	if test == "" {
		return `{"Action":"pass","Package":"` + pkg + `"}`
	}
	return `{"Action":"pass","Package":"` + pkg + `","Test":"` + test + `"}`
}
func fail(pkg, test string) string {
	if test == "" {
		return `{"Action":"fail","Package":"` + pkg + `"}`
	}
	return `{"Action":"fail","Package":"` + pkg + `","Test":"` + test + `"}`
}

// The whole point: a passing run says so with numbers, and exits 0.
func TestCleanRunIsAPositiveClaim(t *testing.T) {
	r := feed(t, pass("p", "TestA"), pass("p", "TestB"), pass("p", ""))
	if code := r.report(io.Discard, nil, time.Second, "/tmp/x"); code != 0 {
		t.Fatalf("exit=%d want 0", code)
	}
	if r.Passed != 2 {
		t.Errorf("passed=%d want 2", r.Passed)
	}
}

// NO TESTS is a failure. Silence here is indistinguishable from success,
// which is the confusion this tool exists to prevent: a bad package
// selector must not look like a clean run.
func TestZeroTestsIsFailure(t *testing.T) {
	r := feed(t)
	if code := r.report(io.Discard, nil, time.Second, "/tmp/x"); code == 0 {
		t.Fatal("an empty run reported success")
	}
}

// A build failure is a failure, not an absence of tests.
func TestBuildFailureIsFailure(t *testing.T) {
	r := feed(t, `{"Action":"output","Package":"p","Output":"FAIL\tp [build failed]\n"}`)
	if code := r.report(io.Discard, nil, time.Second, "/tmp/x"); code == 0 {
		t.Fatal("a build failure reported success")
	}
	if len(r.BuildErrors) == 0 {
		t.Error("build error not captured")
	}
}

// The failure that actually happened on 2026-08-10: `go test` exits
// non-zero — a panic, a timeout, a package-level failure — while no
// individual test is marked failed. That must never read as green.
func TestNonZeroExitWithNoFailedTestIsStillFailure(t *testing.T) {
	r := feed(t, pass("p", "TestA"), pass("p", ""))
	code := r.report(io.Discard, errExit{}, time.Second, "/tmp/x")
	if code == 0 {
		t.Fatal("non-zero go test exit reported as success")
	}
}

// A failing package with passing tests inside it is still a failure.
func TestFailedPackageCountsEvenWithPassingTests(t *testing.T) {
	r := feed(t, pass("p", "TestA"), fail("p", "TestB"), fail("p", ""))
	if code := r.report(io.Discard, errExit{}, time.Second, "/tmp/x"); code == 0 {
		t.Fatal("failing package reported success")
	}
	if len(r.FailedTests) != 1 || !strings.Contains(r.FailedTests[0], "TestB") {
		t.Errorf("failed tests not named: %v", r.FailedTests)
	}
}

// Log noise from PASSING tests must not be mistaken for failure — the
// inverse error, and the reason the old grep approach was unusable.
func TestPassingTestOutputDoesNotCreateFailures(t *testing.T) {
	r := feed(t,
		`{"Action":"output","Package":"p","Test":"TestA","Output":"ERROR: this is a log line saying FAIL\n"}`,
		pass("p", "TestA"), pass("p", ""))
	if code := r.report(io.Discard, nil, time.Second, "/tmp/x"); code != 0 {
		t.Fatalf("exit=%d want 0 — log noise from a passing test is not a failure", code)
	}
}

// Unparseable lines are kept, never dropped in silence.
func TestUnparseableLinesAreNotDiscardedSilently(t *testing.T) {
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "t.log"))
	if err != nil {
		t.Fatal(err)
	}
	r := &result{Packages: map[string]bool{}, transcript: f}
	r.consume([]byte("# gtdemo\n./broken.go:1:1: syntax error"))
	f.Close()
	body, _ := os.ReadFile(filepath.Join(dir, "t.log"))
	if !strings.Contains(string(body), "syntax error") {
		t.Error("an unparseable line vanished instead of reaching the transcript")
	}
}

type errExit struct{}

func (errExit) Error() string { return "exit status 1" }
