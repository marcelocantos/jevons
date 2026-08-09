// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"slices"
)

// Session-identity variables a parent Claude Code session exports to its
// children (🎯T282). jevonsd inherits them whenever it is started from
// inside a Claude Code session — `make run`, `make test-journey`, or any
// dev loop driven by an agent — and then passes them on to every Claude
// agent it spawns, because claudia launches the TUI with the daemon's
// environment.
//
// A spawned Claude Code that inherits these does not start as an
// independent session: it re-joins the parent's session/agent bridge, and
// the fleet worker's terminal stops behaving like a private TUI (observed:
// a directed turn typed into the composer is never submitted, so the
// direct hangs until its caller times out).
//
// Only identity/bridge variables belong here. Configuration variables
// (ANTHROPIC_*, CLAUDE_CODE_MAX_*, provider or model selection, …) are the
// owner's deliberate settings and must survive untouched.
var inheritedSessionEnv = []string{
	"AI_AGENT",
	"CLAUDECODE",
	"CLAUDE_CODE_BRIDGE_SESSION_ID",
	"CLAUDE_CODE_CHILD_SESSION",
	"CLAUDE_CODE_ENTRYPOINT",
	"CLAUDE_CODE_EXECPATH",
	// An experimental flag the enclosing session opted into, not a jevonsd
	// setting: inherited, it makes a fleet worker's private TUI behave as a
	// teams client of the parent session.
	"CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS",
	"CLAUDE_CODE_SESSION_ID",
	"CLAUDE_PID",
}

// IsInheritedSessionEnv reports whether name identifies the enclosing
// agent session rather than configuring one. Pure oracle for the scrub.
func IsInheritedSessionEnv(name string) bool {
	return slices.Contains(inheritedSessionEnv, name)
}

// ScrubInheritedSessionEnv removes the enclosing session's identity from
// this process's environment and returns the names it cleared, sorted.
// Children spawned afterwards — every Claude fleet agent — then start as
// their own sessions. Safe and empty-returning when jevonsd was started
// normally (brew services, a plain shell).
func ScrubInheritedSessionEnv() []string {
	var cleared []string
	for _, name := range inheritedSessionEnv {
		if _, ok := os.LookupEnv(name); !ok {
			continue
		}
		if err := os.Unsetenv(name); err != nil {
			continue
		}
		cleared = append(cleared, name)
	}
	slices.Sort(cleared)
	return cleared
}
