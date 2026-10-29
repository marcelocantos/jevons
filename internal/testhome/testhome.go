// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package testhome moves a test binary off the owner's home directory before
// any test runs. A test package imports it for effect, from a _test.go file:
//
//	import _ "github.com/marcelocantos/jevons/internal/testhome"
//
// Jevons keeps its state under ~/.jevons, and the claudia library it links
// keeps its own under $XDG_STATE_HOME/claudia, falling back to
// ~/.local/state/claudia. On 2026-10-19 a clean `make test-go` gate appended
// fixture session ids (s-po, t763-codex-sid, s-jv-t1020-auto, …) to chain
// files in the running development broker's chains directory: a fleet or
// mcpserver test started a direct claudia agent, and claudia registered its
// chain under the real HOME. testbroker.DeadEnd keeps tests off the broker's
// socket; nothing kept them out of its directory, or out of ~/.jevons.
//
// The init below points HOME and the XDG base directories at a fresh private
// directory, so every default resolves there for this process and every
// child it starts. That directory lives under the user cache directory
// (~/Library/Caches/jevons-testhome on macOS), not under /tmp or
// /private/var/folders: claudia's sidecar seatbelt always grants those, so a
// HOME inside them would make "a write outside the workspace" writable. A
// test binary cannot clean up after m.Run without a TestMain, so each run
// removes the directories of runs whose process has exited. The go, bun and npm caches and the git identity keep their
// real locations: they are caches and configuration, not product state, and
// a test that shells out to `go build` or `npm ci` must not fetch the world
// again.
//
// A live run (any *_LIVE variable set, as for testbroker.DeadEnd) is left
// alone: it means to reach real providers. The ratchet in this package's
// tests requires every test binary in the module to link this one.
package testhome

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// AccountHomeEnv carries the home directory the process started with, for
// the rare test that must read (never write) something only the real
// account has.
const AccountHomeEnv = "JEVONS_TEST_ACCOUNT_HOME"

// Dir is the private home this process runs under, or "" in a live run.
var Dir string

func init() {
	if !testing.Testing() {
		panic("testhome is for test binaries only; it rewrites HOME")
	}
	if LiveRun(os.Environ()) {
		return
	}
	home := os.Getenv("HOME")
	dir, err := privateHome("jevons-testhome")
	if err != nil {
		panic(err)
	}

	// Pin each cache and config to the location it resolves to now, before
	// HOME moves, unless the caller already chose one.
	keep := map[string]string{
		"GOPATH":                filepath.Join(home, "go"),
		"BUN_INSTALL_CACHE_DIR": filepath.Join(home, ".bun", "install", "cache"),
		"npm_config_cache":      filepath.Join(home, ".npm"),
		"GIT_CONFIG_GLOBAL":     filepath.Join(home, ".gitconfig"),
		// 🎯T1309: the Lean toolchain. Under a private HOME elan found none
		// and downloaded all of Lean 4.35 on every run of the T863 oracle;
		// gate b5934142 failed on that download's SSL error.
		"ELAN_HOME":    filepath.Join(home, ".elan"),
		AccountHomeEnv: home,
	}
	if cache, err := os.UserCacheDir(); err == nil {
		keep["GOCACHE"] = filepath.Join(cache, "go-build")
		keep["PLAYWRIGHT_BROWSERS_PATH"] = filepath.Join(cache, "ms-playwright")
	}
	if config, err := os.UserConfigDir(); err == nil {
		keep["GOENV"] = filepath.Join(config, "go", "env")
	}
	for name, path := range keep {
		if os.Getenv(name) == "" && home != "" {
			_ = os.Setenv(name, path)
		}
	}

	set := map[string]string{
		"HOME":            dir,
		"XDG_STATE_HOME":  filepath.Join(dir, ".local", "state"),
		"XDG_CONFIG_HOME": filepath.Join(dir, ".config"),
		"XDG_DATA_HOME":   filepath.Join(dir, ".local", "share"),
		"XDG_CACHE_HOME":  filepath.Join(dir, ".cache"),
	}
	for name, path := range set {
		_ = os.Setenv(name, path)
	}
	// `make test-go` inherits a jobserver MAKEFLAGS token the go test process
	// does not serve. Any nested `make` then blocks forever on the pipe
	// (🎯T1176: TestT438CleanCheckoutUISuiteInvocable looked like a subprocess
	// I/O hang). Drop the inherited make parallel state for this process and
	// every child it starts.
	_ = os.Unsetenv("MAKEFLAGS")
	_ = os.Unsetenv("MFLAGS")
	Dir = dir

}

// LiveRun reports whether env (in os.Environ form) opts into a live run: any
// *_LIVE variable with a value, the rule testbroker.DeadEnd uses.
func LiveRun(env []string) bool {
	for _, kv := range env {
		name, val, _ := strings.Cut(kv, "=")
		if strings.HasSuffix(name, "_LIVE") && val != "" {
			return true
		}
	}
	return false
}

// privateHome makes a fresh directory for this process under
// <user cache dir>/<name>, after removing those of processes that have
// exited. Without a user cache dir it falls back to the temp dir.
func privateHome(name string) (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return os.MkdirTemp("", name+"-")
	}
	base := filepath.Join(cache, name)
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", err
	}
	entries, _ := os.ReadDir(base)
	for _, e := range entries {
		pidText, _, ok := strings.Cut(e.Name(), "-")
		pid, err := strconv.Atoi(pidText)
		if !ok || err != nil || pid <= 0 {
			continue
		}
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			_ = os.RemoveAll(filepath.Join(base, e.Name()))
		}
	}
	return os.MkdirTemp(base, fmt.Sprintf("%d-", os.Getpid()))
}
