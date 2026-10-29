// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/gate"
)

func TestT1198AbnormalGateIsNotTrunkRed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		known   bool
		exit    int
		verdict gate.Verdict
	}{
		{"killed", false, 0, gate.VerdictKilled},
		{"panic after test failure", true, 1, gate.VerdictSuspect},
		{"outage", true, 2, gate.VerdictRed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := &gate.Record{StatusKnown: tc.known, ExitStatus: tc.exit, Verdict: tc.verdict, Command: []string{"go", "test", "./..."}, OutputTail: "--- FAIL: TestEarlierFailure (0.00s)"}
			// An abnormal termination must remain the original gate failure without
			// running a baseline or publishing a trunk incident. An invalid repo makes
			// accidentally reaching either side effect fail this assertion.
			f, err := classifyTrunkFailure("not-a-repository", "tip", t.TempDir(), rec)
			if f != nil || err != nil {
				t.Fatalf("abnormal gate classified as trunk: %+v %v", f, err)
			}
		})
	}
}

func TestT1198LongLandingDoesNotBlockFaultVisibility(t *testing.T) {
	repo := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", "-b", "master", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %s %v", out, err)
	}
	f := &TrunkFault{Kind: TrunkRed, ID: strings.Repeat("a", 64), Base: "tip", Tests: []string{"TestBroken"}, MergeGate: "merge", BaseGate: "base"}
	if err := SaveTrunkFault(repo, f); err != nil {
		t.Fatal(err)
	}
	unlock, err := lockLandings(repo)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	called := false
	if err := DeliverTrunkFault(repo, func(f *TrunkFault) { called = true; f.OwnerNotified = true }); err != nil {
		t.Fatalf("landing starved owner notice: %v", err)
	}
	if !called {
		t.Fatal("owner notice not delivered")
	}
}

func TestT1198PlainFailureLogCannotInventPackageIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gate.log")
	if err := os.WriteFile(path, []byte("--- FAIL: TestShared (0.00s)\nFAIL example.com/a\n"), 0600); err != nil {
		t.Fatal(err)
	}
	rec := &gate.Record{StatusKnown: true, ExitStatus: 1, Verdict: gate.VerdictRed, Command: []string{"go", "test", "./a", "./b"}, OutputPath: path}
	f, err := classifyTrunkFailure("not-a-repository", "tip", t.TempDir(), rec)
	if f != nil || err != nil {
		t.Fatalf("plain output invented package identity: %+v %v", f, err)
	}
}

func TestT1198UnusableLogDoesNotPublishPartialFailureIdentities(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gate.log")
	event := `{"Action":"fail","Package":"example.com/a","Test":"TestShared"}` + "\n"
	if err := os.WriteFile(path, []byte(event+strings.Repeat("x", maxTestEventLineBytes+1)), 0600); err != nil {
		t.Fatal(err)
	}
	rec := &gate.Record{StatusKnown: true, ExitStatus: 1, Verdict: gate.VerdictRed, Command: []string{"go", "test", "-json", "./a"}, OutputPath: path}
	f, err := classifyTrunkFailure("not-a-repository", "tip", t.TempDir(), rec)
	if f != nil || err == nil {
		t.Fatalf("oversized log published partial identities: %+v %v", f, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	f, err = classifyTrunkFailure("not-a-repository", "tip", t.TempDir(), rec)
	if f != nil || err == nil {
		t.Fatalf("missing log invented identities: %+v %v", f, err)
	}
}
