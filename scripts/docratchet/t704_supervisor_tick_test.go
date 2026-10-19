// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

// 🎯T704 — the unattended supervisor tick can read its own memory and the
// gate records it must verify. A bare `claude -p` under a non-interactive
// session is refused access to ~/.jevons and to blurter (approval prompts
// nobody can answer). The production runner must therefore:
//
//  1. --add-dir $HOME/.jevons so notes.md and gates/ are in reach
//  2. --permission-mode bypassPermissions so blurter/Bash are not gated
//  3. --disallowedTools Edit Write NotebookEdit so "work goes through the
//     fleet" is harness-enforced, not brief-trusted
//
// And every entry point (one-shot, loop, supervisord program) must go
// through that runner — a second bare `claude -p` re-opens the hole.

func TestT704RunTickCarriesTheBlindnessFix(t *testing.T) {
	body := readRepo(t, "supervisor/run-tick.sh")
	for _, m := range []string{
		"🎯T704",
		"--add-dir",
		"$HOME/.jevons",
		"--permission-mode",
		"bypassPermissions",
		"--disallowedTools",
		"Edit",
		"Write",
		"NotebookEdit",
		"</dev/null", // supervisord never-closing stdin footgun
	} {
		if !strings.Contains(body, m) {
			t.Errorf("supervisor/run-tick.sh missing 🎯T704 marker %q", m)
		}
	}
	// Must not be a commented-out relic next to a bare invocation.
	if strings.Count(body, "claude") < 1 && strings.Count(body, "CLAUDE_BIN") < 1 {
		t.Error("run-tick.sh does not invoke claude")
	}
}

func TestT704EntryPointsDelegateToRunTick(t *testing.T) {
	// One-shot and loop must call run-tick.sh, not invent their own claude -p.
	for _, path := range []string{
		"supervisor/jevons-tick",
		"supervisor/jevons-tick-loop",
	} {
		body := readRepo(t, path)
		if !strings.Contains(body, "run-tick.sh") {
			t.Errorf("%s does not call run-tick.sh — a bare claude -p would re-open 🎯T704", path)
		}
		// A second direct claude -p (not via run-tick) is the regression.
		for _, line := range strings.Split(body, "\n") {
			trim := strings.TrimSpace(line)
			if strings.HasPrefix(trim, "#") {
				continue
			}
			if strings.Contains(trim, "claude -p") || strings.Contains(trim, "claude -p ") {
				t.Errorf("%s invokes claude -p directly (%q); route through run-tick.sh", path, trim)
			}
		}
	}
}

func TestT704SupervisordProgramPointsAtTrackedLoop(t *testing.T) {
	body := readRepo(t, "supervisor/jevons-tick.ini")
	for _, m := range []string{
		"🎯T704",
		"@REPO@/supervisor/jevons-tick-loop",
		"[program:jevons-tick]",
	} {
		if !strings.Contains(body, m) {
			t.Errorf("supervisor/jevons-tick.ini missing %q", m)
		}
	}
	// Must not point at the untracked ~/.local/bin copy as the sole source.
	if strings.Contains(body, "command=/Users/") || strings.Contains(body, "command=~/") {
		t.Error("jevons-tick.ini hardcodes a home path; use @REPO@ so install.sh renders it")
	}
}

func TestT704AlarmStillPagesOnStaleHeartbeat(t *testing.T) {
	// Sibling of T405: the alarm is what notices the loop is gone. Without
	// it a T704-fixed loop that later dies is still silent.
	body := readRepo(t, "supervisor/jevons-tick-alarm")
	for _, m := range []string{
		"🎯T405",
		"blurter send",
		"jevons-supervisor",
		"heartbeat",
		"STALE_AFTER",
	} {
		if !strings.Contains(body, m) {
			t.Errorf("supervisor/jevons-tick-alarm missing %q", m)
		}
	}
}
