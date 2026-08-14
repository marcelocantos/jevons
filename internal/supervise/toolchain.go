// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package supervise

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// 🎯T434 — the supervisor's restart must not depend on a toolchain the
// launchd environment cannot reach.
//
// THE GUARANTEE THIS FILE MAKES, and the failure it accepts.
//
// The restart script builds bin/detach, bin/runlock and bin/buildsnap on
// demand and fails closed (exit 2) when it cannot, because restarting
// unserialised, or in a session the caller's death can sweep, is the
// failure 🎯T392.5 and 🎯T405 exist to remove. Building needs `go`, which
// on this machine lives in /opt/homebrew/bin. A LaunchAgent whose plist
// says nothing about the environment runs with launchd's own PATH, which
// contains neither Homebrew nor anything else the owner installed — so the
// watchdog could not build the helper, refused every restart, and could
// not even say so: `blurter`, its only channel to the owner while the
// cockpit is down, is on the same unreachable PATH. The supervisor and its
// own alarm shared one failure, and it was latent only because both
// helpers happened to be on disk. `make clean` would have made it real.
//
// CHOSEN: the plist carries an EnvironmentVariables PATH, computed at
// install time from where the tools actually are (AgentPATH below), rather
// than hard-coding /opt/homebrew/bin or trusting bin/ to stay populated.
// Install refuses when it cannot find `go` at all, because a watchdog
// installed without one is a job that will fail every 30 seconds.
//
// ACCEPTED FAILURE: that PATH is a snapshot, true when written. Move the
// toolchain afterwards and it is wrong — the same shape as every other
// snapshot-of-the-environment bug in this repo (🎯T376 / 🎯T377 / 🎯T432).
// It is not left silent: RestartBlocker sees at run time that the helpers
// are absent and no `go` is reachable, and the watchdog puts that in the
// out-of-band notice, so a restart that can never succeed reaches the
// owner with the reason and the fix (`make watchdog-install`) instead of
// looping unheard.

// LaunchdDefaultPATH is what a LaunchAgent gets when its plist declares no
// environment. Everything the owner installed — Homebrew, Go, blurter — is
// somewhere else.
const LaunchdDefaultPATH = "/usr/bin:/bin:/usr/sbin:/sbin"

// RestartTools are the executables the supervised restart path needs and
// launchd cannot see by default.
//
//	go      the restart script builds its helpers on demand
//	blurter the only way the owner hears anything while jevonsd is down
var RestartTools = []string{"go", "blurter"}

// LookPath is exec.LookPath's shape, taken as a parameter so the policy is
// testable without depending on what the test machine has installed.
type LookPath func(string) (string, error)

// AgentPATH computes the PATH to write into the LaunchAgent: launchd's own
// default plus the directory of each tool that could be found. Tools that
// could not be found are returned by name; the caller decides which of
// those are fatal.
//
// Directories, not a copy of the caller's whole PATH: the plist should say
// why each entry is there, and an inherited login PATH carries dozens of
// entries that have nothing to do with restarting a daemon.
func AgentPATH(lookPath LookPath, tools []string) (path string, missing []string) {
	dirs := strings.Split(LaunchdDefaultPATH, ":")
	seen := map[string]bool{}
	for _, d := range dirs {
		seen[d] = true
	}
	for _, tool := range tools {
		p, err := lookPath(tool)
		if err != nil {
			missing = append(missing, tool)
			continue
		}
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		dir := filepath.Dir(p)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}
	return strings.Join(dirs, ":"), missing
}

// RestartHelpers are the binaries under <repo>/bin that the restart script
// re-execs itself through, in the order it needs them.
var RestartHelpers = []string{"detach", "runlock"}

// RestartBlocker names why an attempted restart cannot succeed, or returns
// empty when nothing stands in the way.
//
// The question is narrow on purpose: a helper that is present will be used
// whatever the PATH says, and a missing one is only a problem when there is
// no `go` to build it with. Anything else that can go wrong with a restart
// shows up as the script's own exit status, which the watchdog reports.
func RestartBlocker(repo string, lookPath LookPath, pathEnv string) string {
	need := RestartHelpers
	// buildsnap is only reached when there is no binary to fall back on;
	// the watchdog skips the rebuild whenever one exists.
	if !helperPresent(filepath.Join(repo, "bin", "jevonsd")) {
		need = append(append([]string{}, need...), "buildsnap")
	}
	var absent []string
	for _, h := range need {
		if !helperPresent(filepath.Join(repo, "bin", h)) {
			absent = append(absent, "bin/"+h)
		}
	}
	if len(absent) == 0 {
		return ""
	}
	if _, err := lookPath("go"); err == nil {
		return ""
	}
	return fmt.Sprintf("%s missing under %s and no `go` on PATH (%s) to build %s — "+
		"the restart fails closed rather than run unserialised. Run `make watchdog-install` "+
		"from that repo to give the LaunchAgent a PATH that reaches the toolchain.",
		strings.Join(absent, " and "), repo, pathEnv, plural(len(absent), "it", "them"))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func helperPresent(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}
