// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

// TestT553DailyStaysHEAD ratchets T553.1 / T505: ui/dist is built from
// the HEAD snapshot, not the shared clone.
func TestT553DailyStaysHEAD(t *testing.T) {
	body := readRepo(t, "scripts/restart-jevonsd.sh")
	for _, m := range []string{
		"🎯T553.1",
		"committed HEAD snapshot",
		"-target bin/jevonsd -artifact bin/jevonsd",
		"React is built, type-checked and embedded",
	} {
		if !strings.Contains(body, m) {
			t.Errorf("restart script missing T553.1 marker %q", m)
		}
	}
	if strings.Contains(body, `cd "$ROOT/ui" && npx vite build`) {
		t.Error("restart script still vite-builds ui/dist from the shared tree")
	}
}

// TestT1111RestartScriptNeverTouchesLaunchd ratchets 🎯T1111: supervisord
// owns the development daemon, so the restart script asks it for the
// upgrade exit and otherwise detaches a daemon of its own — it never
// bootstraps, kickstarts or boots out a launchd job, and the Makefile has
// no plist to install.
func TestT1111RestartScriptNeverTouchesLaunchd(t *testing.T) {
	body := readRepo(t, "scripts/restart-jevonsd.sh")
	for _, m := range []string{
		"🎯T1111",
		"upgrade_with_supervisor",
		"start_daemon_detached",
	} {
		if !strings.Contains(body, m) {
			t.Errorf("restart script missing T1111 marker %q", m)
		}
	}
	for _, gone := range []string{
		"launchctl ",
		"start_or_adopt_daemon",
		"-install-daemon-agent",
		"com.marcelocantos.jevonsd",
		"LaunchAgents",
	} {
		if strings.Contains(body, gone) {
			t.Errorf("restart script still carries the launchd path (%q), retired by 🎯T1111", gone)
		}
	}
	mk := readRepo(t, "Makefile")
	for _, gone := range []string{"jevonsd-install", "-install-daemon-agent", "launchctl"} {
		if strings.Contains(mk, gone) {
			t.Errorf("Makefile still carries the launchd path (%q), retired by 🎯T1111", gone)
		}
	}
}

// TestT632ActivateUnlessDestructive ratchets T632: owner-visible work is
// activated in the same turn unless destructive. T553.2 remains as the
// observation-not-script-success gate.
func TestT632ActivateUnlessDestructive(t *testing.T) {
	for _, path := range []string{
		"internal/config/persona.md",
		"AGENTS.md",
		"agents-guide.md",
		"internal/mcpserver/fleet_brief.go",
	} {
		body := readRepo(t, path)
		if !strings.Contains(body, "T632") {
			t.Errorf("%s missing T632", path)
		}
		if !strings.Contains(body, "T553.2") {
			t.Errorf("%s missing T553.2 observation gate", path)
		}
		if !strings.Contains(body, "destructive") {
			t.Errorf("%s missing destructive skip", path)
		}
	}
}

// TestT638CommitWhenDoneVoidsAskFirst ratchets the override of the
// harness default "only commit when asked" (🎯T638).
func TestT638CommitWhenDoneVoidsAskFirst(t *testing.T) {
	for _, path := range []string{
		"internal/config/persona.md",
		"AGENTS.md",
		"agents-guide.md",
		"internal/mcpserver/fleet_brief.go",
		".cursor/rules/commit-when-done.mdc",
	} {
		body := readRepo(t, path)
		if !strings.Contains(body, "T638") {
			t.Errorf("%s missing T638", path)
		}
		if !strings.Contains(body, "only commit when asked") {
			t.Errorf("%s missing voided harness phrase", path)
		}
	}
}

// TestT1122OwnerVisibleMeansOwnerRelevant ratchets the attention doctrine:
// every surface the fleet reads its rules from says that an owner-visible
// thing must be a decision, a fault, or a change in what the owner would do
// next, and names the first applications so the rule is not left abstract.
func TestT1122OwnerVisibleMeansOwnerRelevant(t *testing.T) {
	for _, path := range []string{
		"internal/config/persona.md",
		"AGENTS.md",
		"agents-guide.md",
		"internal/cli/help_agent.md",
		"internal/mcpserver/fleet_brief.go",
	} {
		body := readRepo(t, path)
		for _, m := range []string{"T1122", "decision", "fault", "what they would do next"} {
			if !strings.Contains(body, m) {
				t.Errorf("%s missing 🎯T1122 doctrine marker %q", path, m)
			}
		}
	}
	for _, path := range []string{"AGENTS.md", "agents-guide.md", "internal/mcpserver/fleet_brief.go"} {
		body := readRepo(t, path)
		for _, m := range []string{"T1121", "T1053", "T1120"} {
			if !strings.Contains(body, m) {
				t.Errorf("%s does not name 🎯T1122's application %s", path, m)
			}
		}
	}
}
