// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package supervise

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// 🎯T434 — a restart must not depend on a toolchain the supervisor's
// environment cannot reach.
//
// The restart script builds bin/detach, bin/runlock and bin/buildsnap on
// demand and fails closed (exit 2) when it cannot, because restarting
// unserialised, or in a session the caller's death can sweep, is the
// failure 🎯T392.5 and 🎯T405 exist to remove. Building needs `go`, which
// on this machine lives in /opt/homebrew/bin. A supervisor that starts the
// watchdog with a bare system PATH cannot build the helper, refuses every
// restart, and cannot say so either: `blurter`, its only channel to the
// owner while the cockpit is down, is on the same unreachable PATH. The
// supervisor and its own alarm share one failure, latent only while both
// helpers happen to be on disk; `make clean` makes it real.
//
// supervisord's program sections declare their PATH explicitly
// (supervisor/jevonsd.ini `environment=`), which is where the toolchain is
// made reachable. RestartBlocker is the audible half: at run time it sees
// that the helpers are absent with no `go` on PATH, and the watchdog puts
// that in the out-of-band notice, so a restart that can never succeed
// reaches the owner with the reason and the fix instead of looping unheard.

// LookPath is exec.LookPath's shape, taken as a parameter so the policy is
// testable without depending on what the test machine has installed.
type LookPath func(string) (string, error)

// RestartHelpers are the binaries under <repo>/bin that the restart script
// needs and (like buildsnap below) will `go build` on demand when they are
// absent — the set 🎯T606 exists to keep honest as the script grows.
//
// detach, runlock: re-exec'd through, in the order the script needs them.
// claudiapin: 🎯T448 hard-fails the whole restart ("refusing a silent
// claudia pin") when it is missing and cannot be built — the same
// no-go-on-PATH failure this file exists to name, so it belongs here.
//
// buildident is deliberately NOT listed: the script's own fallback
// (`|| return 0`) treats a failed build as silently skippable, so a
// missing `go` degrades that one check rather than blocking the restart.
// supervisorpolicy (🎯T1086) is not listed either: an unbuildable one
// leaves the supervisord policy "unknown", and the script still starts a
// program supervisord leaves EXITED after the upgrade exit.
var RestartHelpers = []string{"detach", "runlock", "claudiapin"}

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
		"the restart fails closed rather than run unserialised. Give the supervisor's program "+
		"a PATH that reaches the Go toolchain (supervisor/jevonsd.ini environment=) and retry.",
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
