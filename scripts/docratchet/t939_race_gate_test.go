// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"regexp"
	"strings"
	"testing"
)

// TestT939RaceStepStaysInTheStandingGates ratchets the wiring of the race
// step. internal/mcpserver regressed under -race while `make test-go` stayed
// green, because no gate ran the race detector; a race-clean package with no
// gate holding it clean is clean only until the next goroutine lands. So the
// check is that the standing gates run it, not that it passed once.
func TestT939RaceStepStaysInTheStandingGates(t *testing.T) {
	mk := readRepo(t, "Makefile")
	if !regexp.MustCompile(`(?m)^RACE_PKGS \?=.*\./internal/mcpserver`).MatchString(mk) {
		t.Errorf("Makefile: RACE_PKGS no longer names ./internal/mcpserver")
	}
	recipe := regexp.MustCompile(`(?ms)^test-go: [^\n]*\n((?:\t[^\n]*\n)+)`).FindStringSubmatch(mk)
	if recipe == nil {
		t.Fatalf("Makefile: no test-go recipe found")
	}
	if !strings.Contains(recipe[1], "bin/gotest -race") || !strings.Contains(recipe[1], "$(RACE_PKGS)") {
		t.Errorf("Makefile: test-go recipe no longer runs bin/gotest -race over $(RACE_PKGS):\n%s", recipe[1])
	}

	ci := readRepo(t, ".github/workflows/ci.yml")
	if !regexp.MustCompile(`go test -race [^\n]*\./internal/mcpserver`).MatchString(ci) {
		t.Errorf("ci.yml: no go test -race step over ./internal/mcpserver")
	}
}
