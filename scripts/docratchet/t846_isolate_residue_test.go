// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// 🎯T846: host residue from finished isolates must not accumulate.
//
// The journey suite and hermetic tests that touch claudia may start a
// "claudia-anchor" tmux server on a private socket under a temp dir
// (jevons-journey-*, jevons-broker-*, mcpserver-test-*, mcpserver-tmux-*).
// When teardown is skipped (defer + os.Exit), those servers are reparented
// to launchd and live for the machine's uptime. This ratchet fails the
// suite when any such leftover is visible on the host — so a regression
// that re-opens the leak is citable, not ambient noise for a supervisor.
//
// The owner's real claudia socket (~/.local/state/claudia/tmux.sock or
// $XDG_STATE_HOME/claudia/tmux.sock) is never counted: only temp-path
// anchors that name a known isolate/test prefix.

var t846IsolateSock = regexp.MustCompile(
	`(?i)/(?:jevons-journey-|jevons-broker-|mcpserver-test-|mcpserver-tmux-|t\d{6,})[^ ]*/(?:claudia/)?(?:tmux\.sock|dead\.sock|s\.sock)`,
)

func TestT846NoLeftoverIsolateAnchorsOnHost(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX process census")
	}
	if _, err := exec.LookPath("ps"); err != nil {
		t.Skip("ps not on PATH")
	}
	out, err := exec.Command("ps", "-Ao", "pid=,args=").Output()
	if err != nil {
		t.Fatalf("ps: %v", err)
	}
	var leaked []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, "claudia-anchor") {
			continue
		}
		// Never flag the owner's standing claudia server.
		if strings.Contains(line, filepath.Join(".local", "state", "claudia", "tmux.sock")) {
			continue
		}
		if strings.Contains(line, "/.jevons/") {
			continue
		}
		if t846IsolateSock.FindString(line) == "" {
			// Unknown socket shape — still a claudia-anchor on a non-owner
			// path is residue we want to know about.
			if strings.Contains(line, "/var/folders/") || strings.Contains(line, "/tmp/") {
				leaked = append(leaked, line)
			}
			continue
		}
		leaked = append(leaked, line)
	}
	if len(leaked) > 0 {
		t.Errorf("%d leftover claudia-anchor tmux server(s) from finished isolates/tests (🎯T846):\n  %s\n"+
			"Teardown must kill the private socket on every exit path; sweep the host once after fixing.",
			len(leaked), strings.Join(leaked, "\n  "))
	}
}

// Source ratchet: journey stop() and cleanups must both reclaim the private
// tmux, and the happy path must go through exitNow so cleanups run.
func TestT846JourneySuiteTeardownOnEveryExit(t *testing.T) {
	main := readRepo(t, "scripts/journey-suite/main.go")
	if !strings.Contains(main, "killPrivateClaudiaTmux(stateDir)") {
		t.Error("main.go no longer kills private claudia tmux")
	}
	// stop() body must include the kill (exitNow path).
	stopAt := strings.Index(main, "stop := func() {")
	if stopAt < 0 {
		t.Fatal("stop() missing")
	}
	rest := main[stopAt:]
	end := strings.Index(rest, "cleanups.Add(stop)")
	if end < 0 {
		t.Fatal("stop not registered with cleanups")
	}
	if !strings.Contains(rest[:end], "killPrivateClaudiaTmux(stateDir)") {
		t.Error("stop() does not killPrivateClaudiaTmux — exitNow orphans anchors (🎯T846)")
	}
	// Sandbox remove must be on cleanups, not only a defer (defers skip exitNow).
	if !strings.Contains(main, "cleanups.Add(func() {") || !strings.Contains(main, "os.RemoveAll(stateDir)") {
		t.Error("sandbox RemoveAll is not on the cleanups path — fatal/signal leave jevons-journey-* dirs (🎯T846)")
	}
	if strings.Contains(main, "defer os.RemoveAll(stateDir)") {
		t.Error("sandbox RemoveAll still only deferred — that is the 🎯T846 leak")
	}
	// Happy path must exitNow so cleanups run.
	if !strings.Contains(main, "exitNow(0)") {
		t.Error("PASS path does not exitNow(0) — cleanups never run on green (🎯T846)")
	}
}
