// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/marcelocantos/jevons/internal/capacity"
)

// Env keys for the heavy-suite CPU budget (🎯T781). Gate sets them on every
// heavy child so a nested gotest / go test that re-reads the environment sees
// the same knob the capacity governor owns — not a second, local constant.
const (
	// HeavySuiteMaxProcsEnv overrides Policy.HeavySuiteMaxProcs for one run.
	// "0" or empty falls through to capacity.json / the shipped default.
	HeavySuiteMaxProcsEnv = "JEVONS_HEAVY_SUITE_MAX_PROCS"
	// HeavySuiteUnlimitedEnv=1 opts a run out of the budget entirely — the
	// escape hatch when a human deliberately wants the whole machine.
	HeavySuiteUnlimitedEnv = "JEVONS_HEAVY_SUITE_UNLIMITED"
	// HeavySuitePkgParEnv is set by gate so nested helpers can read the
	// derived -p without re-deriving; callers should not need to set it.
	HeavySuitePkgParEnv = "JEVONS_HEAVY_SUITE_PKG_PAR"
)

// applyHeavySuiteBudget puts the capacity governor's single CPU knob onto a
// heavy gate child (🎯T781): GOMAXPROCS, go test / gotest -p when missing, and
// a mild nice when the budget leaves cores free for interactive work.
//
// The original argv on the Record is left alone — attestation names what the
// caller asked for. The cmd is what actually runs.
func applyHeavySuiteBudget(cmd *exec.Cmd, argv []string) {
	if cmd == nil || unlimitedHeavySuite() {
		return
	}
	b := resolveHeavySuiteBudget()
	env := cmd.Env
	if env == nil {
		env = os.Environ()
	}
	env = setEnvKV(env, "GOMAXPROCS", strconv.Itoa(b.MaxProcs))
	env = setEnvKV(env, HeavySuiteMaxProcsEnv, strconv.Itoa(b.MaxProcs))
	env = setEnvKV(env, HeavySuitePkgParEnv, strconv.Itoa(b.PackageParallel))
	cmd.Env = env

	newArgs := injectPackageParallel(argv, b.PackageParallel)
	if b.Nice > 0 {
		newArgs = wrapNice(newArgs, b.Nice)
	}
	if len(newArgs) == 0 {
		return
	}
	// Re-resolve Path when we rewrote argv0 (nice wrap, or a bare name).
	bin := newArgs[0]
	if filepath.Base(bin) == bin {
		if looked, err := exec.LookPath(bin); err == nil {
			bin = looked
		}
	}
	cmd.Path = bin
	cmd.Args = newArgs
}

func unlimitedHeavySuite() bool {
	v := strings.TrimSpace(os.Getenv(HeavySuiteUnlimitedEnv))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

// resolveHeavySuiteBudget is the live look-up: env override, then
// ~/.jevons/capacity.json, then the shipped default. Pure resolve stays in
// capacity so oracles do not need a filesystem.
func resolveHeavySuiteBudget() capacity.HeavySuiteBudget {
	cores := runtime.NumCPU()
	pol := capacity.DefaultPolicy()
	if home, err := os.UserHomeDir(); err == nil {
		path := filepath.Join(home, ".jevons", capacity.ConfigFileName)
		if loaded, err := capacity.LoadPolicy(path); err == nil {
			pol = loaded
		}
	}
	if raw := strings.TrimSpace(os.Getenv(HeavySuiteMaxProcsEnv)); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			pol.HeavySuiteMaxProcs = n
		}
	}
	return capacity.ResolveHeavySuiteBudget(pol, cores)
}

// injectPackageParallel adds -p N to a go test / gotest argv that does not
// already name one. make targets are left alone: the Makefile already passes
// TEST_PKG_PAR, and inventing flags for make would be a different command.
func injectPackageParallel(argv []string, p int) []string {
	if len(argv) == 0 || p < 1 {
		return append([]string(nil), argv...)
	}
	out := append([]string(nil), argv...)
	base := filepath.Base(out[0])
	switch base {
	case "go":
		if !hasGoTest(out) || hasFlag(out, "-p") {
			return out
		}
		// Insert after "test": go test -p N …
		for i := 1; i < len(out); i++ {
			if out[i] == "test" {
				return insertAfter(out, i, "-p", strconv.Itoa(p))
			}
		}
		return out
	case "gotest":
		if hasFlag(out, "-p") {
			return out
		}
		// gotest [-race] [-timeout d] [-p n] packages…
		return insertAfter(out, 0, "-p", strconv.Itoa(p))
	default:
		return out
	}
}

func hasGoTest(argv []string) bool {
	for _, a := range argv[1:] {
		if a == "test" {
			return true
		}
	}
	return false
}

func hasFlag(argv []string, flag string) bool {
	for i := 1; i < len(argv); i++ {
		a := argv[i]
		if a == flag {
			return true
		}
		if strings.HasPrefix(a, flag+"=") {
			return true
		}
	}
	return false
}

func insertAfter(argv []string, idx int, parts ...string) []string {
	out := make([]string, 0, len(argv)+len(parts))
	out = append(out, argv[:idx+1]...)
	out = append(out, parts...)
	out = append(out, argv[idx+1:]...)
	return out
}

// wrapNice rewrites argv as `nice -n N -- original…`. treeguard already
// treats nice as a transparent prefix; gate's HeavyCommand classifier still
// sees the caller's original argv before this rewrite.
func wrapNice(argv []string, n int) []string {
	if len(argv) == 0 || n == 0 {
		return argv
	}
	out := make([]string, 0, len(argv)+4)
	out = append(out, "nice", "-n", fmt.Sprintf("%d", n), "--")
	out = append(out, argv...)
	return out
}

// setEnvKV replaces or appends KEY=value in an env slice.
func setEnvKV(env []string, key, value string) []string {
	prefix := key + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			out := append([]string(nil), env...)
			out[i] = prefix + value
			return out
		}
	}
	return append(append([]string(nil), env...), prefix+value)
}
