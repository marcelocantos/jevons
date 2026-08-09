// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cli_test

import (
	"os"
	"slices"
	"testing"

	"github.com/marcelocantos/jevons/internal/cli"
)

// TestScrubInheritedSessionEnvClearsSessionIdentity: a jevonsd started
// from inside a Claude Code session must not hand that session's identity
// to the Claude agents it spawns (🎯T282) — they would rejoin the parent's
// session bridge instead of running as their own.
func TestScrubInheritedSessionEnvClearsSessionIdentity(t *testing.T) {
	// The test binary may itself be running under Claude Code, so assert on
	// the vars this test sets rather than on the whole cleared list.
	set := map[string]string{
		"CLAUDECODE":                    "1",
		"CLAUDE_CODE_SESSION_ID":        "b57b17b8-e766-4a8d-ad24-8a365800192c",
		"CLAUDE_CODE_BRIDGE_SESSION_ID": "session_01UumiB9NKj9ubXYHg5Gkohh",
		"CLAUDE_PID":                    "20832",
	}
	for name, value := range set {
		t.Setenv(name, value)
	}

	cleared := cli.ScrubInheritedSessionEnv()
	for name := range set {
		if _, ok := os.LookupEnv(name); ok {
			t.Errorf("%s still set after scrub", name)
		}
		if !slices.Contains(cleared, name) {
			t.Errorf("%s missing from cleared list %v", name, cleared)
		}
		if !cli.IsInheritedSessionEnv(name) {
			t.Errorf("IsInheritedSessionEnv(%q) = false; that is session identity", name)
		}
	}
	if !slices.IsSorted(cleared) {
		t.Errorf("cleared = %v, want sorted", cleared)
	}
}

// TestScrubInheritedSessionEnvKeepsConfiguration: model/provider/API
// configuration is the owner's deliberate setting and must survive.
func TestScrubInheritedSessionEnvKeepsConfiguration(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-not-a-real-key")
	t.Setenv("CLAUDE_CODE_MAX_OUTPUT_TOKENS", "8192")
	t.Setenv("JEVONS_PROVIDER", "claude")

	cli.ScrubInheritedSessionEnv()

	for _, name := range []string{"ANTHROPIC_API_KEY", "CLAUDE_CODE_MAX_OUTPUT_TOKENS", "JEVONS_PROVIDER"} {
		if _, ok := os.LookupEnv(name); !ok {
			t.Errorf("%s was scrubbed; only session identity should be", name)
		}
		if cli.IsInheritedSessionEnv(name) {
			t.Errorf("IsInheritedSessionEnv(%q) = true; that is configuration, not identity", name)
		}
	}
}

// TestScrubInheritedSessionEnvNoopOutsideASession: the normal daemon start
// (brew services, plain shell) clears nothing.
func TestScrubInheritedSessionEnvNoopOutsideASession(t *testing.T) {
	for _, name := range []string{
		"AI_AGENT", "CLAUDECODE", "CLAUDE_CODE_BRIDGE_SESSION_ID",
		"CLAUDE_CODE_CHILD_SESSION", "CLAUDE_CODE_ENTRYPOINT",
		"CLAUDE_CODE_EXECPATH", "CLAUDE_CODE_SESSION_ID", "CLAUDE_PID",
	} {
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("unset %s: %v", name, err)
		}
	}
	if cleared := cli.ScrubInheritedSessionEnv(); len(cleared) != 0 {
		t.Fatalf("cleared = %v, want none", cleared)
	}
}
