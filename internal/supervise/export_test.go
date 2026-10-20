// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package supervise

import (
	"errors"
	"strings"
	"testing"
)

// DefaultLaunchctl is the seam as the package built it, captured before
// init below replaces it: package variables initialise before init runs.
var DefaultLaunchctl = launchctl

// No test in this package reaches the owner's launchd (🎯T1095). The
// gui/<uid> domain a test would address is the one the development
// watchdog lives in, so a stray bootstrap or bootout from a test run is a
// change to the owner's machine. The default answer is an empty domain:
// nothing is held and nothing is disabled.
func init() {
	launchctl = func(args ...string) ([]byte, error) {
		return EmptyLaunchd(args...)
	}
	haveLaunchd = true
}

// EmptyLaunchd answers launchctl as a domain holding no jobs and no
// overrides, refusing anything that would change it.
func EmptyLaunchd(args ...string) ([]byte, error) {
	if len(args) == 0 {
		return nil, errors.New("launchctl fake: no arguments")
	}
	switch args[0] {
	case "print":
		return []byte("Could not find service " + strings.Join(args[1:], " ")), errors.New("exit status 113")
	case "print-disabled":
		return []byte("disabled services = {\n}\n"), nil
	}
	return []byte("launchctl fake: " + strings.Join(args, " ") + " refused in tests"), errors.New("exit status 1")
}

// FakeLaunchctl answers launchctl with fn for the rest of the test. Stop
// any goroutine that calls launchctl before the test ends: cleanups run
// last-registered first, so register this before starting the goroutine.
func FakeLaunchctl(t testing.TB, fn func(args ...string) ([]byte, error)) {
	t.Helper()
	prev := launchctl
	launchctl = fn
	t.Cleanup(func() { launchctl = prev })
}
