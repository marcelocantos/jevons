// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

// TestSecondUserInstallDocMarkers is a prose ratchet for 🎯T47 residual
// docs truth: the published README must still describe the stranger
// install path (brew → config → pair residual → adopt → direct) without
// requiring source-code archaeology. Not a live second-user drill.
func TestSecondUserInstallDocMarkers(t *testing.T) {
	readme := readRepo(t, "README.md")
	arch := readRepo(t, "docs/architecture-current.md")

	for _, m := range []string{
		"brew install marcelocantos/tap/jevons",
		"brew services start jevons",
		"Grok CLI",
		"~/.jevons/config.yaml",
		"Adopt a session",
		"Direct an adopted",
		"Pair a device",
		"jevonsd --pair",
		"--add-credential",
		"credential.json",
		"https://carrier-pigeon.fly.dev",
		"http://localhost:13705/",
		"embeds the web UI",
		// Honest residual so a stranger is not sold a finished onboarding.
		"no App Store",
		"jevons --init",
	} {
		if !strings.Contains(readme, m) {
			t.Errorf("README.md missing second-user install marker %q", m)
		}
	}

	// architecture-current must not reintroduce the pre-T6 "all-interfaces
	// by default" claim or the pre-T44 "persona embedded in main.go" story.
	for _, bad := range []string{
		"default bind is all-interfaces",
		"externalizing them is 🎯T44",
		"being deleted (🎯T41)",
	} {
		if strings.Contains(arch, bad) {
			t.Errorf("docs/architecture-current.md still contains stale claim %q", bad)
		}
	}
	for _, m := range []string{
		"loopback-only",
		"127.0.0.1",
		"chatlog/<overseer>.jsonl",
		"structured config",
	} {
		if !strings.Contains(arch, m) {
			t.Errorf("docs/architecture-current.md missing truth-pass marker %q", m)
		}
	}
}
