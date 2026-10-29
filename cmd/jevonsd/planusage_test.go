// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/planusage"
)

// The old journey fixture must fail before starting broker or provider work;
// silently ignoring it would let a hermetic scenario consume live metrics.
func TestPlanUsageRetiredFixtureRefusesStartup(t *testing.T) {
	const childEnv = "JEVONS_TEST_RETIRED_PLAN_FIXTURE"
	if os.Getenv(childEnv) == "1" {
		os.Args = []string{"jevonsd"}
		main()
		return
	}
	t.Setenv(childEnv, "1")
	t.Setenv(planusage.FixtureEnv, "/retired-fixture-must-not-be-read.json")
	cmd := exec.Command(os.Args[0], "-test.run=^TestPlanUsageRetiredFixtureRefusesStartup$")
	output, err := cmd.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
		t.Fatalf("expected startup refusal, got %v: %s", err, output)
	}
	if !strings.Contains(string(output), "JEVONS_PLAN_USAGE_FIXTURE is retired; seed plan usage into the isolate control replica") {
		t.Fatalf("missing migration guidance: %s", output)
	}
}
