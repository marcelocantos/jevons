// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/roles"
)

// TestT5362AuditorRoleFileExists ratchets 🎯T536.2: the built-in auditor
// role file carries read-only / ledger-challenge doctrine, and agents-guide
// names the role mechanism.
func TestT5362AuditorRoleFileExists(t *testing.T) {
	d, err := roles.Builtin(roles.Auditor)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"cannot write or patch product code",
		"silent-decision",
		"do not file bullseye",
		"🎯T536.2",
	} {
		if !strings.Contains(d.Body, want) {
			t.Errorf("auditor.md missing %q", want)
		}
	}
	if !d.ReadOnly {
		t.Error("auditor must be ReadOnly")
	}

	guide := readRepo(t, "agents-guide.md")
	for _, want := range []string{
		"🎯T536.2",
		"role=auditor",
		"internal/roles",
	} {
		if !strings.Contains(guide, want) {
			t.Errorf("agents-guide.md missing T536.2 marker %q", want)
		}
	}
	brief := readRepo(t, "internal/mcpserver/fleet_brief.go")
	if strings.Contains(strings.ToLower(brief), "if you are an auditor") {
		t.Error("FleetStandingBrief must not carry auditor if-you-are doctrine")
	}
}
