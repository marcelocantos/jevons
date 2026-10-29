// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"errors"
	"strings"
	"testing"
)

func t1271NestedCmd() []string {
	return []string{
		"/bin/bash", "-c",
		"cd relayv34 && GOWORK=off go test -race ./... && go vet ./...",
	}
}

func TestT1271NestedModuleGateCoversNestedPackages(t *testing.T) {
	rec := &Record{Command: t1271NestedCmd()}
	if err := CheckLandingGatePackageCoverage(rec, []string{"relayv34", "relayv34/widget"}); err != nil {
		t.Fatalf("nested cd-and-test should cover nested packages: %v", err)
	}
}

func TestT1271NestedModuleGateDoesNotCoverRootPackages(t *testing.T) {
	rec := &Record{Command: t1271NestedCmd()}
	err := CheckLandingGatePackageCoverage(rec, []string{"relayv34", "internal/server"})
	if err == nil {
		t.Fatal("nested cd-and-test must not cover a root-module package")
	}
	if !errors.Is(err, ErrLandingGateCoverage) {
		t.Fatalf("want ErrLandingGateCoverage, got %v", err)
	}
	if !strings.Contains(err.Error(), "internal/server") {
		t.Fatalf("error should name uncovered root package: %v", err)
	}
	if strings.Contains(err.Error(), "uncovered packages:") && strings.Contains(err.Error(), "relayv34,") {
		t.Fatalf("relayv34 should be covered, not listed missing: %v", err)
	}
}

func TestT1271RootGoTestAndMakeTestGoUnchanged(t *testing.T) {
	goRec := &Record{Command: []string{"go", "test", "-count=1", "./internal/server/...", "./internal/fleet/..."}}
	if err := CheckLandingGatePackageCoverage(goRec, []string{"internal/server", "internal/fleet"}); err != nil {
		t.Fatalf("root go test coverage regressed: %v", err)
	}
	makeRec := &Record{Command: []string{"make", "test-go"}}
	if err := CheckLandingGatePackageCoverage(makeRec, []string{"internal/server", "cmd/integrate"}); err != nil {
		t.Fatalf("make test-go coverage regressed: %v", err)
	}
	narrow := &Record{Command: []string{"go", "test", "-run", "TestX", "./internal/server"}}
	err := CheckLandingGatePackageCoverage(narrow, []string{"internal/server", "internal/fleet"})
	if err == nil || !errors.Is(err, ErrLandingGateCoverage) {
		t.Fatalf("want refusal for -run missing fleet, got %v", err)
	}
}

func TestT1271NestedModuleRunFilterStillRefused(t *testing.T) {
	rec := &Record{Command: []string{
		"/bin/bash", "-c",
		"cd relayv34 && go test -run TestX ./...",
	}}
	err := CheckLandingGatePackageCoverage(rec, []string{"relayv34"})
	if err == nil || !errors.Is(err, ErrLandingGateCoverage) {
		t.Fatalf("want refusal for nested -run, got %v", err)
	}
	if !strings.Contains(err.Error(), "-run") {
		t.Fatalf("error should name the -run filter: %v", err)
	}
}

func TestT1271NonTestBashStillBadCommand(t *testing.T) {
	rec := &Record{Command: []string{"/bin/bash", "-c", "echo hi"}}
	err := CheckLandingGatePackageCoverage(rec, []string{"internal/server"})
	if err == nil || !errors.Is(err, ErrLandingGateCoverage) {
		t.Fatalf("want bad-command refusal, got %v", err)
	}
	if !strings.Contains(err.Error(), "must be go test or make test-go") {
		t.Fatalf("error should still name the command requirement: %v", err)
	}
}

func TestT1271NestedModuleGoTestArgvRewritesPatterns(t *testing.T) {
	got, ok := nestedModuleGoTestArgv(t1271NestedCmd())
	if !ok {
		t.Fatal("specimen bash -c form should unwrap")
	}
	wantPrefix := []string{"go", "test"}
	if len(got) < 3 || got[0] != wantPrefix[0] || got[1] != wantPrefix[1] {
		t.Fatalf("got %v", got)
	}
	var pkgs []string
	for _, a := range got[2:] {
		if !strings.HasPrefix(a, "-") {
			pkgs = append(pkgs, a)
		}
	}
	if len(pkgs) != 1 || pkgs[0] != "./relayv34/..." {
		t.Fatalf("rewritten packages = %v, want [./relayv34/...]; argv=%v", pkgs, got)
	}
}

func TestT1271BashLcAndBareBashUnwrap(t *testing.T) {
	for _, argv := range [][]string{
		{"bash", "-c", "cd nested && go test ./..."},
		{"/bin/bash", "-lc", "cd nested && go test ./..."},
	} {
		got, ok := nestedModuleGoTestArgv(argv)
		if !ok {
			t.Fatalf("should unwrap %v", argv)
		}
		if !containsStr(got, "./nested/...") {
			t.Fatalf("argv %v unwrapped to %v, missing ./nested/...", argv, got)
		}
	}
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
