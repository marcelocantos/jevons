// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

// TestRelaySelfHostDocMarkers is a prose ratchet for 🎯T156: a stranger
// can obtain a usable pigeon relay from published docs without
// contacting the author (self-host + URL/token auth model). Not a live
// Fly deploy or pair smoke.
func TestRelaySelfHostDocMarkers(t *testing.T) {
	readme := readRepo(t, "README.md")
	arch := readRepo(t, "docs/architecture-current.md")

	for _, m := range []string{
		// Auth model named (URL + token).
		"Auth model (URL + token)",
		"PIGEON_TOKEN",
		"TERN_TOKEN",
		"--relay-token",
		// Scope: self-host only — no free-tier claim for strangers.
		"self-host only",
		"self-host-only",
		"not a public free tier",
		// No author messaging for tokens.
		"never message the author",
		"do not ask the author for TERN_TOKEN",
		// Copy-paste self-host from public pigeon.
		"github.com/marcelocantos/pigeon",
		"go build -o pigeon ./cmd/pigeon",
		"openssl rand -hex 32",
		"YOUR-RELAY-HOST",
		"Running the Relay Server",
		"running-the-relay-server",
		// Historical hostname is named as not-stranger free tier.
		"https://carrier-pigeon.fly.dev",
	} {
		// Markers may span a single soft-wrapped line; collapse newlines
		// for the "Running the Relay Server" heading that wraps.
		hay := readme
		if strings.Contains(m, "Running the Relay") {
			hay = strings.Join(strings.Fields(readme), " ")
			m = strings.Join(strings.Fields(m), " ")
		}
		if !strings.Contains(hay, m) {
			t.Errorf("README.md missing T156 relay marker %q", m)
		}
	}

	// Must not invent a stranger-usable free Fly claim or author-token path.
	for _, bad := range []string{
		"free public relay at https://carrier-pigeon.fly.dev",
		"sign up for carrier-pigeon",
		"message the author for TERN_TOKEN",
		"ask the author for a token",
	} {
		if strings.Contains(readme, bad) {
			t.Errorf("README.md contains forbidden free-tier/author-token claim %q", bad)
		}
	}

	for _, m := range []string{
		"self-hosted",
		"PIGEON_TOKEN",
		"TERN_TOKEN",
		"T156",
	} {
		if !strings.Contains(arch, m) {
			t.Errorf("docs/architecture-current.md missing T156 marker %q", m)
		}
	}
}
