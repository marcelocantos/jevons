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
	body := readRepo(t, "scripts/restart-daily-jevonsd.sh")
	for _, m := range []string{
		"🎯T553.1",
		"committed HEAD snapshot",
		"snap_ui=",
		"$SNAP_DIR/ui",
	} {
		if !strings.Contains(body, m) {
			t.Errorf("restart script missing T553.1 marker %q", m)
		}
	}
	if strings.Contains(body, `cd "$ROOT/ui" && npx vite build`) {
		t.Error("restart script still vite-builds ui/dist from the shared tree")
	}
}

// TestT553KeepAliveOwnsDaemon ratchets T553.3: script prefers
// com.marcelocantos.jevonsd KeepAlive over nohup-start.
func TestT553KeepAliveOwnsDaemon(t *testing.T) {
	body := readRepo(t, "scripts/restart-daily-jevonsd.sh")
	for _, m := range []string{
		"🎯T553.3",
		"com.marcelocantos.jevonsd",
		"start_or_adopt_daemon",
		"KeepAlive",
		"refusing to bootstrap a wrong job",
	} {
		if !strings.Contains(body, m) {
			t.Errorf("restart script missing T553.3 marker %q", m)
		}
	}
	mk := readRepo(t, "Makefile")
	for _, m := range []string{
		"jevonsd-install",
		"com.marcelocantos.jevonsd",
		"-install-daemon-agent",
	} {
		if !strings.Contains(mk, m) {
			t.Errorf("Makefile missing T553.3 target %q", m)
		}
	}
}

// TestT553WorkersDoNotAutoBounce ratchets T553.2 doctrine.
func TestT553WorkersDoNotAutoBounce(t *testing.T) {
	for _, path := range []string{
		"internal/config/persona.md",
		"AGENTS.md",
		"agents-guide.md",
		"internal/mcpserver/fleet_brief.go",
	} {
		body := readRepo(t, path)
		if !strings.Contains(body, "T553.2") {
			t.Errorf("%s missing T553.2", path)
		}
		if !strings.Contains(body, "do not invoke") && !strings.Contains(body, "Workers do not invoke") {
			t.Errorf("%s missing workers-do-not-invoke doctrine", path)
		}
	}
}
