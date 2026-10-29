// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"errors"
	"strings"
	"testing"
)

// 🎯T1314: a repo's declared full gate (Claudia's make gate-checks, or a
// root-level bash -c that runs go test ./...) covers every changed package.
// A make target that is not a full suite, and a nested-module cd, still do not.

func TestT1314MakeGateChecksCoversEveryPackage(t *testing.T) {
	rec := &Record{Command: []string{"make", "gate-checks"}}
	if err := CheckLandingGatePackageCoverage(rec, []string{"internal/sidecar", "cmd/claudia"}); err != nil {
		t.Fatalf("make gate-checks should cover every package: %v", err)
	}
	gmake := &Record{Command: []string{"gmake", "-j4", "gate-checks"}}
	if err := CheckLandingGatePackageCoverage(gmake, []string{"pkg/foo"}); err != nil {
		t.Fatalf("gmake gate-checks should cover every package: %v", err)
	}
}

func TestT1314RootBashGoTestDotDotDotCoversEveryPackage(t *testing.T) {
	rec := &Record{Command: []string{
		"/bin/bash", "-c",
		"GOWORK=off go test -race ./... && go vet ./...",
	}}
	if err := CheckLandingGatePackageCoverage(rec, []string{"internal/sidecar", "cmd/claudia"}); err != nil {
		t.Fatalf("root bash go test ./... should cover every package: %v", err)
	}
}

func TestT1314NonFullSuiteMakeStillRefused(t *testing.T) {
	rec := &Record{Command: []string{"make", "ui-build"}}
	err := CheckLandingGatePackageCoverage(rec, []string{"internal/server"})
	if err == nil || !errors.Is(err, ErrLandingGateCoverage) {
		t.Fatalf("want refusal for make ui-build, got %v", err)
	}
	if !strings.Contains(err.Error(), "must be go test") {
		t.Fatalf("error should name the command requirement: %v", err)
	}
}

func TestT1314NestedCdStillNotRootFullSuite(t *testing.T) {
	rec := &Record{Command: []string{
		"/bin/bash", "-c",
		"cd relayv34 && go test -race ./...",
	}}
	err := CheckLandingGatePackageCoverage(rec, []string{"internal/server"})
	if err == nil || !errors.Is(err, ErrLandingGateCoverage) {
		t.Fatalf("nested cd-and-test must not cover a root package, got %v", err)
	}
}

func TestT1314AchieveAcceptsMakeGateChecks(t *testing.T) {
	fix := "dddddddddddddddddddddddddddddddddddddddd"
	rec := t1162GreenRecord("gatechecks", fix, []string{"make", "gate-checks"})
	att := fix + " GATE make-gate-checks exit=0 GREEN id=gatechecks"
	r := t1162Check(att, rec)
	if r.Verdict != AchieveVerified {
		t.Fatalf("verdict=%s want verified: %s", r.Verdict, r.Detailed())
	}
}
