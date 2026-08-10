// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestT405SelfDetachIsUnconditional ratchets the property, not the advice:
// the restart script re-execs into its own session before it does anything,
// so a caller who forgets to detach cannot cause an outage. The hazard was
// documented as an instruction to callers from the script's first version,
// and on 2026-08-10 the one caller who forgot took the fleet down.
func TestT405SelfDetachIsUnconditional(t *testing.T) {
	body := readRepo(t, "scripts/restart-daily-jevonsd.sh")

	for _, m := range []string{
		"SELF-DETACH",
		"🎯T405",
		`exec "$DETACH"`,
		"JEVONS_RESTART_DETACHED",
		"cannot build $DETACH", // fails closed rather than running attached
		"JEVONS_RESTART_FAULT", // the seam the oracle kills it through
		"jevons-watchdog",      // points at its own supervisor
		"JEVONS_RESTART_BIN",   // so the oracle can drive a stub daemon
		"is not the daily",     // and leave brew alone on a scratch port
	} {
		if !strings.Contains(body, m) {
			t.Errorf("restart-daily-jevonsd.sh missing 🎯T405 marker %q", m)
		}
	}

	// The re-exec must come before the lock: the lock has to be held from
	// inside the detached session, or killing the caller takes the holder.
	detach := strings.Index(body, `exec "$DETACH"`)
	lock := strings.Index(body, `exec "$RUNLOCK"`)
	if detach < 0 || lock < 0 {
		t.Fatalf("cannot locate both re-execs (detach=%d lock=%d)", detach, lock)
	}
	if detach > lock {
		t.Error("the runlock re-exec happens before the self-detach; a killed caller would take the lock holder with it")
	}
}

// TestT405SupervisorIsBuiltAndInstallable: an unwired supervisor is worth
// zero, so `make all` builds it and there is a documented way to load it.
func TestT405SupervisorIsBuiltAndInstallable(t *testing.T) {
	mk := readRepo(t, "Makefile")
	for _, m := range []string{
		"bin/detach",
		"bin/jevons-watchdog",
		"watchdog-install",
		"watchdog-status",
		"./cmd/detach",
		"./cmd/jevons-watchdog",
	} {
		if !strings.Contains(mk, m) {
			t.Errorf("Makefile missing 🎯T405 target %q", m)
		}
	}
	for _, line := range strings.Split(mk, "\n") {
		if strings.HasPrefix(line, "all:") {
			for _, want := range []string{"detach", "jevons-watchdog"} {
				if !strings.Contains(line, want) {
					t.Errorf("`make all` does not build %s: %q", want, line)
				}
			}
		}
	}
}

// TestT405OracleIsCommitted: the target's oracle is two tests that kill a
// real restart — one mid-bounce, one by SIGKILLing its caller's process
// group — plus the control that shows the second still detects the
// regression. An oracle nobody can run is not one.
func TestT405OracleIsCommitted(t *testing.T) {
	for _, path := range []string{
		"cmd/detach/main.go",
		"cmd/jevons-watchdog/main.go",
		"cmd/jevons-watchdog/install.go",
		"cmd/jevons-watchdog/oracle_test.go",
		"internal/supervise/policy.go",
		"internal/supervise/store.go",
		"internal/supervise/report.go",
		"internal/supervise/launchd.go",
		"internal/supervise/policy_test.go",
		"internal/supervise/store_test.go",
	} {
		out, err := exec.Command("git", "-C", repoRoot(t), "ls-files", "--error-unmatch", path).CombinedOutput()
		if err != nil {
			t.Errorf("%s is not committed (%v): %s", path, err, strings.TrimSpace(string(out)))
		}
	}

	oracle := readRepo(t, "cmd/jevons-watchdog/oracle_test.go")
	for _, m := range []string{
		"func TestWatchdogRecoversARestartThatDiedMidBounce",
		"func TestForegroundRestartSurvivesItsCallerBeingKilled",
		"func TestForegroundRestartDiesWithoutSelfDetach",
		"syscall.SIGKILL",
		"JEVONS_RESTART_FAULT=after-kill",
	} {
		if !strings.Contains(oracle, m) {
			t.Errorf("🎯T405 oracle missing %q", m)
		}
	}
	if !strings.Contains(oracle, "dailyPort = 13705") {
		t.Error("the oracle must name the daily port it refuses to run against")
	}
}

// TestT405DoctrineMarkers: the fleet is told that detaching is now
// preferred rather than load-bearing, and that a supervisor exists.
func TestT405DoctrineMarkers(t *testing.T) {
	for _, doc := range []struct{ name, body string }{
		{"AGENTS.md", readRepo(t, "AGENTS.md")},
		{"agents-guide.md", readRepo(t, "agents-guide.md")},
		{"internal/config/persona.md", readRepo(t, "internal/config/persona.md")},
	} {
		for _, m := range []string{
			"🎯T405",
			"com.marcelocantos.jevons-watchdog",
			"watchdog-install",
			"own session",
		} {
			if !strings.Contains(doc.body, m) {
				t.Errorf("%s missing 🎯T405 doctrine marker %q", doc.name, m)
			}
		}
	}
}
