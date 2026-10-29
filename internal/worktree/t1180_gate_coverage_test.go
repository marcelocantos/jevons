// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/gate"
	"github.com/marcelocantos/jevons/internal/worktree"
)

// Oracles for 🎯T1180: integrate refuses a tip gate that did not run an
// unfiltered go test over every Go package the worker branch changed.

func savePristineGateRecord(t *testing.T, root, id, commit string, command []string) {
	t.Helper()
	store, err := gate.OpenStore(root)
	if err != nil {
		t.Fatalf("open gate store: %v", err)
	}
	rec := &gate.Record{
		ID:          id,
		Name:        "t1180-test",
		Command:     command,
		Started:     time.Now(),
		Ended:       time.Now().Add(time.Second),
		ExitStatus:  0,
		StatusKnown: true,
		Verdict:     gate.VerdictGreen,
		Tree: &gate.TreeProvenance{
			Repo:   root,
			Commit: commit,
			Clean:  true,
			Shared: &gate.SharedTreeState{Repo: root},
		},
	}
	if err := store.Save(rec); err != nil {
		t.Fatalf("save gate record: %v", err)
	}
}

func workerGoPackagesCommit(t *testing.T, base, agent string) (string, string) {
	t.Helper()
	wt, err := worktree.Ensure(base, agent)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	writeFile(t, filepath.Join(wt, "internal/server/srv.go"), "package server\n\nfunc S() {}\n")
	writeFile(t, filepath.Join(wt, "internal/fleet/flt.go"), "package fleet\n\nfunc F() {}\n")
	git(t, wt, "add", "internal/server/srv.go", "internal/fleet/flt.go")
	git(t, wt, "commit", "-q", "-m", agent+" touches server and fleet")
	return wt, git(t, wt, "rev-parse", "HEAD")
}

func TestRequireLandingGateRefusesNarrowRunMissingPackages(t *testing.T) {
	base := sharedClone(t)
	_, sha := workerGoPackagesCommit(t, base, "jv-narrow")
	gateDir := t.TempDir()
	savePristineGateRecord(t, gateDir, "g-narrow", sha, []string{
		"go", "test", "-run", "TestX", "./internal/server",
	})

	_, err := worktree.RequireLandingGate(base, "jv-narrow", gateDir, "")
	if err == nil {
		t.Fatal("expected refusal for -run gate missing fleet package")
	}
	if !errors.Is(err, gate.ErrLandingGateCoverage) {
		t.Fatalf("expected ErrLandingGateCoverage, got %v", err)
	}
	if !strings.Contains(err.Error(), "internal/fleet") {
		t.Fatalf("error should name uncovered fleet package: %v", err)
	}
	if !strings.Contains(err.Error(), "bin/gate -clean") {
		t.Fatalf("error should suggest a gate command: %v", err)
	}
}

func TestRequireLandingGateLandsWhenGateCoversChangedPackages(t *testing.T) {
	base := sharedClone(t)
	_, sha := workerGoPackagesCommit(t, base, "jv-covered")
	gateDir := t.TempDir()
	savePristineGateRecord(t, gateDir, "g-full", sha, []string{
		"go", "test", "-count=1", "./internal/server/...", "./internal/fleet/...",
	})

	rec, err := worktree.RequireLandingGate(base, "jv-covered", gateDir, "")
	if err != nil {
		t.Fatalf("RequireLandingGate: %v", err)
	}
	if rec == nil || rec.ID != "g-full" {
		t.Fatalf("expected accepted gate record, got %+v", rec)
	}
	if _, err := worktree.Integrate(&worktree.IntegrateArgs{BaseWorkdir: base, AgentName: "jv-covered"}); err != nil {
		t.Fatalf("Integrate: %v", err)
	}
	mustReach(t, base, sha)
}

func workerNestedModuleCommit(t *testing.T, base, agent string, alsoRoot bool) (string, string) {
	t.Helper()
	wt, err := worktree.Ensure(base, agent)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	writeFile(t, filepath.Join(wt, "relayv34/go.mod"), "module example.com/x/relayv34\n\ngo 1.24\n")
	writeFile(t, filepath.Join(wt, "relayv34/wid.go"), "package relayv34\n\nfunc W() {}\n")
	files := []string{"relayv34/go.mod", "relayv34/wid.go"}
	if alsoRoot {
		writeFile(t, filepath.Join(wt, "internal/server/srv.go"), "package server\n\nfunc S() {}\n")
		files = append(files, "internal/server/srv.go")
	}
	git(t, wt, append([]string{"add"}, files...)...)
	git(t, wt, "commit", "-q", "-m", agent+" nested module")
	return wt, git(t, wt, "rev-parse", "HEAD")
}

func t1271NestedGateCmd() []string {
	return []string{
		"/bin/bash", "-c",
		"cd relayv34 && GOWORK=off go test -race ./... && go vet ./...",
	}
}

func TestRequireLandingGateAcceptsNestedModuleCdAndTest(t *testing.T) {
	base := sharedClone(t)
	_, sha := workerNestedModuleCommit(t, base, "jv-nested", false)
	gateDir := t.TempDir()
	savePristineGateRecord(t, gateDir, "g-nested", sha, t1271NestedGateCmd())

	rec, err := worktree.RequireLandingGate(base, "jv-nested", gateDir, "")
	if err != nil {
		t.Fatalf("RequireLandingGate: %v", err)
	}
	if rec == nil || rec.ID != "g-nested" {
		t.Fatalf("expected accepted nested-module gate, got %+v", rec)
	}
}

func TestRequireLandingGateNestedModuleGateDoesNotCoverRootDiff(t *testing.T) {
	base := sharedClone(t)
	_, sha := workerNestedModuleCommit(t, base, "jv-nested-root", true)
	gateDir := t.TempDir()
	savePristineGateRecord(t, gateDir, "g-nested-only", sha, t1271NestedGateCmd())

	_, err := worktree.RequireLandingGate(base, "jv-nested-root", gateDir, "")
	if err == nil {
		t.Fatal("expected refusal: nested-module gate cannot cover a root-module package")
	}
	if !errors.Is(err, gate.ErrLandingGateCoverage) {
		t.Fatalf("expected ErrLandingGateCoverage, got %v", err)
	}
	if !strings.Contains(err.Error(), "internal/server") {
		t.Fatalf("error should name uncovered root package: %v", err)
	}
}
