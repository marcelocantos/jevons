// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package launchdfake answers for launchd in a test binary, so no test
// reaches the owner's real one (🎯T1097). A test package imports it for
// effect, from a _test.go file:
//
//	import _ "github.com/marcelocantos/jevons/internal/launchdfake"
//
// Every launchctl exec in the module goes through the internal/supervise
// seam, and the domain a test would address is gui/<uid> — the one the
// owner's development watchdog and daemon jobs live in. A stray
// bootstrap, bootout or kickstart from a test run is a change to the
// owner's machine, and even a read answers about the owner's jobs rather
// than the test's.
//
// The init below replaces the seam with a recorder whose default answer
// is an empty domain: nothing is held, nothing is disabled, and anything
// that would change the domain is refused. A test that needs launchd to
// say something else swaps the answer with Use; Calls reports what the
// binary asked for. The ratchet in this package's tests requires every
// test binary that links internal/supervise to link this package, and no
// Go file outside the seam to name launchctl.
//
// The seam covers this process. Child processes a test starts — a built
// watchdog, the restart script — exec launchctl by name; internal/testhome
// puts a refusing stub first on the PATH they inherit.
package launchdfake

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/marcelocantos/jevons/internal/supervise"
)

// Responder answers one launchctl invocation as launchctl would: the
// combined output and a non-nil error for a non-zero exit.
type Responder func(args ...string) ([]byte, error)

var (
	mu      sync.Mutex
	respond Responder = Empty
	calls   [][]string
)

func init() {
	if !testing.Testing() {
		panic("launchdfake is for test binaries only; it replaces launchd")
	}
	supervise.ReplaceLaunchctl(answer)
}

func answer(args ...string) ([]byte, error) {
	mu.Lock()
	calls = append(calls, append([]string(nil), args...))
	fn := respond
	mu.Unlock()
	return fn(args...)
}

// Empty answers launchctl as a domain holding no jobs and no overrides,
// refusing anything that would change it.
func Empty(args ...string) ([]byte, error) {
	if len(args) == 0 {
		return nil, errors.New("launchdfake: no arguments")
	}
	switch args[0] {
	case "print", "list":
		return []byte("Could not find service " + strings.Join(args[1:], " ")), errors.New("exit status 113")
	case "print-disabled":
		return []byte("disabled services = {\n}\n"), nil
	}
	return []byte("launchdfake: launchctl " + strings.Join(args, " ") + " refused in tests"), errors.New("exit status 1")
}

// Use answers launchctl with fn for the rest of the test. Calls are still
// recorded. Stop any goroutine that calls launchctl before the test ends:
// cleanups run last-registered first, so call Use before starting it.
func Use(t testing.TB, fn Responder) {
	t.Helper()
	mu.Lock()
	prev := respond
	respond = fn
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		respond = prev
		mu.Unlock()
	})
}

// Calls returns every launchctl invocation this binary has made, in
// order, each as its argument list.
func Calls() [][]string {
	mu.Lock()
	defer mu.Unlock()
	out := make([][]string, len(calls))
	for i, c := range calls {
		out[i] = append([]string(nil), c...)
	}
	return out
}
