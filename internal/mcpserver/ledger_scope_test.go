// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

// twoRepoFixture writes two repos, each with its own bullseye ledger holding
// the SAME target id, and registers one work agent in each bound to that id.
// This is the shape 🎯T389 is about: ids are allocated per ledger, so T19
// names different work in each repo, and the fleet holds both at once.
func twoRepoFixture(t *testing.T, id string) (repoA, repoB string, reg *claudia.Registry) {
	t.Helper()
	root := t.TempDir()
	ledger := "schema_version: 1\ntargets:\n  " + id + ":\n    name: local work\n    status: open\n"
	for _, name := range []string{"claudia", "orthograph"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte(ledger), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	repoA = filepath.Join(root, "claudia")
	repoB = filepath.Join(root, "orthograph")

	reg, err := claudia.NewRegistry(filepath.Join(root, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{
		{Name: "cl-t19-worker", WorkDir: repoA, SessionID: "s-a", Purpose: claudia.PurposeWork,
			Parent: "claudia-po", Materialized: true, Provider: "grok", TargetID: id},
		{Name: "og-t19-worker", WorkDir: repoB, SessionID: "s-b", Purpose: claudia.PurposeWork,
			Parent: "og-po", Materialized: true, Provider: "grok", TargetID: id},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	return repoA, repoB, reg
}

// 🎯T389 oracle (guard half): the engagement guard answers per ledger. Red
// against the pre-fix tree, where workAgentsEngagedOnTarget scanned the whole
// registry on TargetID equality alone and each repo saw the other's worker.
func TestT389EngagementGuardIsScopedToItsLedger(t *testing.T) {
	repoA, repoB, reg := twoRepoFixture(t, "T19")

	// Control: the same fixture asked the pre-🎯T389 question — unscoped —
	// still answers with both repos. A fixture that stopped colliding would
	// make every scoped assertion below vacuous.
	if got := workAgentsEngagedOnTarget(reg, "T19", "", ""); len(got) != 2 {
		t.Fatalf("unscoped T19=%v want both repos' workers — fixture no longer collides", got)
	}

	if got := workAgentsEngagedOnTarget(reg, "T19", repoA, ""); len(got) != 1 || got[0] != "cl-t19-worker" {
		t.Fatalf("claudia T19 engaged=%v want [cl-t19-worker]", got)
	}
	if got := workAgentsEngagedOnTarget(reg, "T19", repoB, ""); len(got) != 1 || got[0] != "og-t19-worker" {
		t.Fatalf("orthograph T19 engaged=%v want [og-t19-worker]", got)
	}

	// The product consequence: spawning for the second repo's T19 is not
	// refused by the first repo's worker. Refusing it is the silent-drop
	// shape — 🎯T155 then sees no unconsumed leaf and never re-covers it.
	s := New(repoB, nil, nil)
	s.SetRegistry(reg)
	if msg := s.refuseEngagedOrClosedTarget("og-t19-second", repoB, "T19", false); !strings.Contains(msg, "og-t19-worker") {
		t.Fatalf("own repo's worker must still refuse a duplicate: %q", msg)
	}
	// A third repo with the same id has nobody on it — no refusal at all.
	repoC := filepath.Join(t.TempDir(), "jevons")
	if err := os.MkdirAll(repoC, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoC, "bullseye.yaml"),
		[]byte("schema_version: 1\ntargets:\n  T19:\n    name: third\n    status: open\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if msg := s.refuseEngagedOrClosedTarget("jv-t19-worker", repoC, "T19", false); msg != "" {
		t.Fatalf("third repo's T19 must be free to start, got refusal %q", msg)
	}
}

// 🎯T389: a workdir with no ledger of its own still scopes as its own repo
// (git root), and two such repos never merge. The fallbacks exist so a repo
// whose ledger is not committed yet does not silently share a scope with the
// whole filesystem.
func TestT389LedgerKeyNeverMergesTwoRepos(t *testing.T) {
	root := t.TempDir()
	var dirs []string
	for _, name := range []string{"alpha", "beta"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		dirs = append(dirs, dir)
	}
	reg, err := claudia.NewRegistry(filepath.Join(root, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for i, dir := range dirs {
		if err := reg.Register(claudia.AgentDef{
			Name: filepath.Base(dir) + "-worker", WorkDir: dir, SessionID: "s" + string(rune('a'+i)),
			Purpose: claudia.PurposeWork, Parent: "po", Materialized: true, Provider: "grok",
			TargetID: "T7",
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range dirs {
		got := workAgentsEngagedOnTarget(reg, "T7", dir, "")
		want := filepath.Base(dir) + "-worker"
		if len(got) != 1 || got[0] != want {
			t.Fatalf("%s T7 engaged=%v want [%s]", dir, got, want)
		}
	}
}
