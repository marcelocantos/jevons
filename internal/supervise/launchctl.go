// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package supervise

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// LaunchctlCommand is the one launchd client the module execs.
const LaunchctlCommand = "launchctl"

// launchctl is the module's one exec of launchd's client (🎯T1111). It is
// read-only in intent: the only caller left is internal/provider's
// liveness probe, which asks `launchctl list <label>` about jobs that are
// not Jevons — a backup script, a sync — for their LastExitStatus. Jevons
// itself is not supervised by launchd; supervisord owns jevonsd.
//
// In a test binary it does not exec. A test's gui/<uid> domain is the
// owner's, so even a read would answer about the owner's jobs rather than
// the test's; the answer is the empty domain, and a test that needs
// launchd to say something else injects its own command runner.
var launchctl = func(args ...string) ([]byte, error) {
	if testing.Testing() {
		return []byte("Could not find service " + strings.Join(args, " ")),
			fmt.Errorf("supervise: launchctl %s: not reached from a test binary", strings.Join(args, " "))
	}
	return exec.Command(LaunchctlCommand, args...).CombinedOutput()
}

// Launchctl runs one launchctl command through the seam and returns its
// combined output.
func Launchctl(args ...string) ([]byte, error) { return launchctl(args...) }
