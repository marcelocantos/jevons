// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/marcelocantos/jevons/internal/capacity"
	"github.com/marcelocantos/jevons/internal/commitattrib"
	"github.com/marcelocantos/jevons/internal/config"
)

// fleetTestBudgetOff disables 🎯T781 wrapping when set to "1" (tests only).
const fleetTestBudgetOffEnv = "JEVONS_TEST_BUDGET_OFF"

// fleetHeavyTestBudget applies the capacity.json test CPU envelope when a
// fleet seat runs a heavy suite. Returns whether the argv/env were changed.
func fleetHeavyTestBudget(argv []string, baseEnv []string) (bool, []string, []string) {
	if os.Getenv(fleetTestBudgetOffEnv) == "1" {
		return false, argv, baseEnv
	}
	if strings.TrimSpace(os.Getenv(commitattrib.ActorEnv)) == "" {
		return false, argv, baseEnv
	}
	if !HeavyCommand(argv) {
		return false, argv, baseEnv
	}
	budget := loadFleetTestBudget()
	out := append([]string(nil), argv...)
	out = injectTestParallel(out, budget.PkgPar)
	if budget.Nice > 0 {
		out = append([]string{"nice", "-n", fmt.Sprintf("%d", budget.Nice)}, out...)
	}
	env := setEnvVar(baseEnv, "GOMAXPROCS", fmt.Sprintf("%d", budget.MaxProcs))
	return true, out, env
}

func loadFleetTestBudget() capacity.TestBudget {
	dir := jevonsStateDir()
	pol, err := capacity.LoadPolicy(capacity.ConfigPath(dir))
	if err != nil || pol == nil {
		pol = capacity.DefaultPolicy()
	}
	return pol.TestBudget()
}

func jevonsStateDir() string {
	if d := strings.TrimSpace(os.Getenv("JEVONS_STATE_DIR")); d != "" {
		return d
	}
	return config.Default().StateDir
}

func injectTestParallel(argv []string, par int) []string {
	if par <= 0 || len(argv) == 0 || argvHasFlag(argv, "-p") {
		return argv
	}
	switch filepath.Base(argv[0]) {
	case "go":
		for i, a := range argv[1:] {
			if a != "test" {
				continue
			}
			pos := i + 2
			out := append([]string{}, argv[:pos]...)
			out = append(out, "-p", fmt.Sprintf("%d", par))
			out = append(out, argv[pos:]...)
			return out
		}
	case "gotest":
		out := append([]string{argv[0], "-p", fmt.Sprintf("%d", par)}, argv[1:]...)
		return out
	}
	return argv
}

func argvHasFlag(argv []string, flag string) bool {
	for _, a := range argv {
		if a == flag || strings.HasPrefix(a, flag+"=") {
			return true
		}
	}
	return false
}

func setEnvVar(base []string, key, val string) []string {
	prefix := key + "="
	out := make([]string, 0, len(base)+1)
	replaced := false
	for _, e := range base {
		if strings.HasPrefix(e, prefix) {
			out = append(out, prefix+val)
			replaced = true
			continue
		}
		out = append(out, e)
	}
	if !replaced {
		if base == nil {
			out = append(os.Environ(), prefix+val)
		} else {
			out = append(out, prefix+val)
		}
	}
	return out
}
