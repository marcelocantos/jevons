// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/commitattrib"
)

func TestT781FleetHeavyTestBudgetAppliesToGoTest(t *testing.T) {
	t.Setenv(commitattrib.ActorEnv, "jv-t781-auto")
	t.Setenv(fleetTestBudgetOffEnv, "")
	dir := t.TempDir()
	cfg := filepath.Join(dir, "capacity.json")
	if err := os.WriteFile(cfg, []byte(`{"test_max_procs":3,"test_pkg_parallel":2,"test_nice":7}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JEVONS_STATE_DIR", dir)

	argv := []string{"go", "test", "./..."}
	changed, out, env := fleetHeavyTestBudget(argv, nil)
	if !changed {
		t.Fatal("expected budget to apply for fleet seat")
	}
	if out[0] != "nice" || out[1] != "-n" || out[2] != "7" {
		t.Fatalf("nice wrap = %v", out[:4])
	}
	if !strings.Contains(strings.Join(out, " "), "-p 2") {
		t.Fatalf("missing -p 2: %v", out)
	}
	if !envHas(env, "GOMAXPROCS=3") {
		t.Fatalf("env missing GOMAXPROCS=3: %v", env)
	}
}

func TestT781FleetHeavyTestBudgetSkipsWithoutSeat(t *testing.T) {
	t.Setenv(commitattrib.ActorEnv, "")
	changed, out, _ := fleetHeavyTestBudget([]string{"go", "test", "./..."}, nil)
	if changed || len(out) != 3 {
		t.Fatalf("unexpected change without fleet seat: changed=%v out=%v", changed, out)
	}
}

func TestT781FleetHeavyTestBudgetRespectsExplicitP(t *testing.T) {
	t.Setenv(commitattrib.ActorEnv, "jv-t781-auto")
	t.Setenv(fleetTestBudgetOffEnv, "")
	dir := t.TempDir()
	t.Setenv("JEVONS_STATE_DIR", dir)

	argv := []string{"go", "test", "-p", "9", "./..."}
	_, out, _ := fleetHeavyTestBudget(argv, nil)
	if strings.Contains(strings.Join(out, " "), " -p 2 ") {
		t.Fatalf("must not add a second -p: %v", out)
	}
}

func TestT781HeavyCommandIncludesGotestAndTestBinary(t *testing.T) {
	if !HeavyCommand([]string{"bin/gotest", "./..."}) {
		t.Fatal("gotest should be heavy")
	}
	if !HeavyCommand([]string{"./docratchet.test", "-test.v"}) {
		t.Fatal(".test binary should be heavy")
	}
}

func envHas(env []string, want string) bool {
	for _, e := range env {
		if e == want {
			return true
		}
	}
	return false
}
