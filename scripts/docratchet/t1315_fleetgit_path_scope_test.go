// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

// TestT1315FleetgitPathScopeDoctrine fails if standing docs claim bin/fleetgit
// is on every fleet seat's PATH (it is not: an OMP sidecar's git is
// /usr/bin/git; Claudia's sidecar does not wrap git). The live guard is the
// Claude treeguard PreToolUse Bash hook. Keep T1134's "never stash" markers
// intact; this ratchet only pins the PATH-scope honesty (🎯T1315).
func TestT1315FleetgitPathScopeDoctrine(t *testing.T) {
	docs := []string{
		"AGENTS.md",
		"internal/config/persona.md",
		"agents-guide.md",
		"internal/cli/help_agent.md",
		"internal/mcpserver/fleet_brief.go",
	}
	for _, rel := range docs {
		lower := strings.ToLower(readRepo(t, rel))
		if !strings.Contains(lower, "🎯t1315") {
			t.Errorf("%s no longer cites 🎯T1315", rel)
		}
		if !strings.Contains(lower, "not on") || !strings.Contains(lower, "path") {
			t.Errorf("%s must say fleetgit is not on PATH (🎯T1315)", rel)
		}
		if !strings.Contains(lower, "treeguard") || !strings.Contains(lower, "pretooluse") {
			t.Errorf("%s must name the Claude treeguard PreToolUse hook as the live git guard", rel)
		}
		for _, overclaim := range []string{
			"`bin/fleetgit` on path and",
			"ahead of real git on path refuses",
			"fleet seats: `bin/fleetgit`",
			"fleetgit and treeguard refuse",
			"treeguard and fleetgit refuse",
		} {
			if strings.Contains(lower, overclaim) {
				t.Errorf("%s still overclaims fleetgit PATH coverage: %q", rel, overclaim)
			}
		}
	}

	mk := strings.ToLower(readRepo(t, "Makefile"))
	if !strings.Contains(mk, "🎯t1315") || !strings.Contains(mk, "not currently on fleet seats") {
		t.Error("Makefile fleetgit comment must say it is not currently on fleet seats' PATH (🎯T1315)")
	}
	fg := strings.ToLower(readRepo(t, "cmd/fleetgit/main.go"))
	if !strings.Contains(fg, "🎯t1315") || !strings.Contains(fg, "not currently on fleet seats' path") {
		t.Error("cmd/fleetgit/main.go package comment must say it is not currently on fleet seats' PATH (🎯T1315)")
	}
}
