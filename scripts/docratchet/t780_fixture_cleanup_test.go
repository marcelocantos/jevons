// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

// 🎯T780: tests that start detached daemons or load generators must leave
// none behind. The two named offenders were TestRestartThrashPolicy's
// throwaway bin/jevonsd and TestTheInstalledAgentPATH*'s cold-PATH restart
// fixture. T932 armed thrash; this ratchet keeps both fixtures armed and
// keeps the journey suite's private-tmux kill on the exitNow path.

func TestT780FixtureDaemonsAreArmedForEveryExit(t *testing.T) {
	thrash := readRepo(t, "scripts/docratchet/t218_restart_thrash_test.go")
	if !strings.Contains(thrash, "testreap.Arm(t)") {
		t.Error("newThrashEnv lost testreap.Arm — TestRestartThrashPolicy can orphan bin/jevonsd on -timeout (🎯T780/T932)")
	}
	if !strings.Contains(thrash, "testreap.ReapUnder") && !strings.Contains(thrash, "e.killDaemon") {
		t.Error("thrash env lost its normal-exit reap")
	}

	cold := readRepo(t, "cmd/jevons-watchdog/t434_toolchain_test.go")
	if !strings.Contains(cold, "testreap.Arm(t)") {
		t.Error("newColdRepo lost testreap.Arm — TestTheInstalledAgentPATH* can orphan bin/jevonsd on -timeout (🎯T780)")
	}
	if !strings.Contains(cold, "testreap.ReapUnder") && !strings.Contains(cold, "reapUnder") {
		t.Error("newColdRepo lost its normal-exit reap")
	}

	journey := readRepo(t, "scripts/journey-suite/main.go")
	stopAt := strings.Index(journey, "stop := func() {")
	if stopAt < 0 {
		t.Fatal("journey suite stop() missing")
	}
	rest := journey[stopAt:]
	end := strings.Index(rest, "cleanups.Add(stop)")
	if end < 0 {
		t.Fatal("journey suite no longer registers stop with cleanups")
	}
	if !strings.Contains(rest[:end], "killPrivateClaudiaTmux(stateDir)") {
		t.Error("journey stop() does not kill private claudia tmux — exitNow orphans anchors (🎯T780/T846)")
	}
}

// Load generators in this repo must arm a trap (or equivalent) rather than
// a trailing kill that never runs on interrupt. The 38 orphaned
// `while :; do :; done` shells from a trailing-kill stress script held the
// host at load 250 for 19h (🎯T780 acceptance).
func TestT780ShellFixturesTrapTheirChildren(t *testing.T) {
	for _, rel := range []string{
		"scripts/upgrade-drill.sh",
		"scripts/t63-daemon-reclaim.sh",
	} {
		body := readRepo(t, rel)
		if !strings.Contains(body, "trap ") {
			t.Errorf("%s starts fixtures without trap cleanup — interrupt leaves them running (🎯T780)", rel)
		}
	}
}
