// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/jevons/internal/worktree"
)

func TestT1198IntegrateDistinguishesExistingTrunkFailure(t *testing.T) {
	base := t1197Seed(t)
	wt, err := worktree.Ensure(base, "jv-t1198")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(wt, "internal/x/x.go"), "package x\n\nfunc Value() string { return \"ok-worker\" }\n")
	git(t, wt, "add", "internal/x/x.go")
	git(t, wt, "commit", "-q", "-m", "unrelated worker")
	// The worker never touched the failing ratchet. Trunk independently gains
	// the exact failure, like the T742 specimen in the target.
	writeFile(t, filepath.Join(base, "scripts/docratchet/ratchet_test.go"), "package docratchet_test\nimport \"testing\"\nfunc TestT742DoctrineNamesEveryStringMatchingRule(t *testing.T) { t.Fatal(\"trunk regression\") }\n")
	git(t, base, "add", "scripts/docratchet/ratchet_test.go")
	git(t, base, "commit", "-q", "-m", "red trunk")
	tip := git(t, base, "rev-parse", "HEAD")
	store := t.TempDir()
	_, err = worktree.Integrate(&worktree.IntegrateArgs{BaseWorkdir: base, AgentName: "jv-t1198", GateStoreRoot: store})
	var fault *worktree.TrunkFault
	if !errors.As(err, &fault) {
		t.Fatalf("want typed trunk_red, got %v", err)
	}
	if fault.Kind != worktree.TrunkRed || fault.Base != tip || len(fault.Tests) != 1 || fault.Tests[0] != "example.com/integratetest/scripts/docratchet/TestT742DoctrineNamesEveryStringMatchingRule" || fault.MergeGate == fault.BaseGate {
		t.Fatalf("bad evidence: %+v", fault)
	}
	if got := git(t, base, "rev-parse", "HEAD"); got != tip {
		t.Fatalf("failed landing moved trunk: %s", got)
	}
	persisted, err := worktree.ReadTrunkFault(base)
	if err != nil || persisted == nil || persisted.ID != fault.ID {
		t.Fatalf("missing durable incident: %+v %v", persisted, err)
	}
	// Delivery receipts survive subsequent failed landings and ledger-only
	// trunk movement, preventing a new PO turn for every attempted landing.
	err = worktree.DeliverTrunkFault(base, func(f *worktree.TrunkFault) { f.OwnerNotified = true; f.POWoken = true })
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(base, "note.txt"), "ledger-only progress\n")
	git(t, base, "add", "note.txt")
	git(t, base, "commit", "-q", "-m", "note without repair")
	_, err = worktree.Integrate(&worktree.IntegrateArgs{BaseWorkdir: base, AgentName: "jv-t1198", GateStoreRoot: store})
	var again *worktree.TrunkFault
	if !errors.As(err, &again) || again.ID != fault.ID || !again.OwnerNotified || !again.POWoken {
		t.Fatalf("repeat not coalesced: %+v %v", again, err)
	}
}

func TestT1198MergeOnlyFailureIsNotTrunkRed(t *testing.T) {
	base := t1197Seed(t)
	wt, err := worktree.Ensure(base, "jv-regression")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(wt, "internal/x/x.go"), "package x\n\nfunc Value() string { return \"FORBIDDEN\" }\n")
	git(t, wt, "add", "internal/x/x.go")
	git(t, wt, "commit", "-q", "-m", "worker regression")
	writeFile(t, filepath.Join(base, "note.txt"), "trunk moved\n")
	git(t, base, "add", "note.txt")
	git(t, base, "commit", "-q", "-m", "trunk moves")
	_, err = worktree.Integrate(&worktree.IntegrateArgs{BaseWorkdir: base, AgentName: "jv-regression", GateStoreRoot: t.TempDir()})
	var fault *worktree.TrunkFault
	if !errors.Is(err, worktree.ErrPostMergeGate) || errors.As(err, &fault) {
		t.Fatalf("worker failure misclassified: %v", err)
	}
	if persisted, err := worktree.ReadTrunkFault(base); persisted != nil || err != nil {
		t.Fatalf("unexpected incident: %+v %v", persisted, err)
	}
}

func TestT1198RepairClearsPersistedIncident(t *testing.T) {
	base := t1197Seed(t)
	wt, err := worktree.Ensure(base, "jv-t1198-repair")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(wt, "internal/x/x.go"), "package x\nfunc Value() string { return \"ok-worker\" }\n")
	git(t, wt, "add", "internal/x/x.go")
	git(t, wt, "commit", "-q", "-m", "worker")
	writeFile(t, filepath.Join(base, "scripts/docratchet/ratchet_test.go"), "package docratchet_test\nimport \"testing\"\nfunc TestBrokenTrunk(t *testing.T) { t.Fatal(\"broken\") }\n")
	git(t, base, "add", "scripts/docratchet/ratchet_test.go")
	git(t, base, "commit", "-q", "-m", "red trunk")
	args := &worktree.IntegrateArgs{BaseWorkdir: base, AgentName: "jv-t1198-repair", GateStoreRoot: t.TempDir()}
	if _, err := worktree.Integrate(args); err == nil {
		t.Fatal("expected refusal")
	}
	f, err := worktree.ReadTrunkFault(base)
	if err != nil || f == nil {
		t.Fatalf("no incident: %v", err)
	}
	// A second failed attempt against the identical base must reuse its
	// authoritative baseline proof, not run another baseline gate.
	_, err = worktree.Integrate(args)
	var again *worktree.TrunkFault
	if !errors.As(err, &again) || again.BaseGate != f.BaseGate {
		t.Fatalf("did not reuse proof: %+v %v", again, err)
	}
	writeFile(t, filepath.Join(base, "scripts/docratchet/ratchet_test.go"), t1197Ratchet)
	git(t, base, "add", "scripts/docratchet/ratchet_test.go")
	git(t, base, "commit", "-q", "-m", "repair trunk")
	if _, err := worktree.Integrate(args); err != nil {
		t.Fatalf("repair landing failed: %v", err)
	}
	if f, err := worktree.ReadTrunkFault(base); f != nil || err != nil {
		t.Fatalf("stale red incident: %+v %v", f, err)
	}
}

func TestT1198SameTestNameInDifferentPackagesIsNotTrunkRed(t *testing.T) {
	base := goModuleSharedClone(t)
	for _, pkg := range []string{"a", "b"} {
		body := "package " + pkg + "\nimport \"testing\"\nfunc TestShared(t *testing.T) {}\n"
		if pkg == "a" {
			body = "package a\nimport \"testing\"\nfunc TestShared(t *testing.T) { t.Fatal(\"baseline A failure\") }\n"
		}
		writeFile(t, filepath.Join(base, pkg, "shared_test.go"), body)
	}
	git(t, base, "add", "a", "b")
	git(t, base, "commit", "-q", "-m", "baseline A fails B passes")
	wt, err := worktree.Ensure(base, "jv-shared-test-name")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(wt, "a/shared_test.go"), "package a\nimport \"testing\"\nfunc TestShared(t *testing.T) {}\n")
	writeFile(t, filepath.Join(wt, "b/shared_test.go"), "package b\nimport \"testing\"\nfunc TestShared(t *testing.T) { t.Fatal(\"worker B failure\") }\n")
	git(t, wt, "add", "a", "b")
	git(t, wt, "commit", "-q", "-m", "worker fixes A but breaks B")
	writeFile(t, filepath.Join(base, "note.txt"), "trunk moves\n")
	git(t, base, "add", "note.txt")
	git(t, base, "commit", "-q", "-m", "trunk moves")
	_, err = worktree.Integrate(&worktree.IntegrateArgs{BaseWorkdir: base, AgentName: "jv-shared-test-name", GateStoreRoot: t.TempDir()})
	var fault *worktree.TrunkFault
	if !errors.Is(err, worktree.ErrPostMergeGate) || errors.As(err, &fault) {
		t.Fatalf("cross-package false trunk_red: %+v %v", fault, err)
	}
	if persisted, err := worktree.ReadTrunkFault(base); persisted != nil || err != nil {
		t.Fatalf("false persisted incident: %+v %v", persisted, err)
	}
}
