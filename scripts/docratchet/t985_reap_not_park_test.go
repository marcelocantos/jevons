// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

// TestT985ReapNotParkDoctrine ratchets the 🎯T985 convention on every
// instruction surface that describes the finished-work reap: a worker with
// no more work to do is reaped, not parked, and parking is reserved for a
// seat blocked on something external that may resume. The pre-T985 text
// ("deliberate stop without kill still leaves registration for resume") was
// the convention under which jv-t947-plan-token and jv-t972-reap-scope sat
// as standing parked rows on 2026-09-30; it must not come back as the
// default.
func TestT985ReapNotParkDoctrine(t *testing.T) {
	for _, rel := range []string{
		"AGENTS.md",
		"internal/config/persona.md",
		"agents-guide.md",
		"internal/cli/help_agent.md",
		"internal/mcpserver/fleet_brief.go",
	} {
		lower := strings.ToLower(readRepo(t, rel))
		if !strings.Contains(lower, "🎯t985") {
			t.Errorf("%s no longer cites 🎯T985", rel)
			continue
		}
		for _, m := range []string{
			"reaped rather than parked",
			"blocked on something external",
			"superseded",
			"jevons_agent_kill",
		} {
			if !strings.Contains(lower, m) {
				t.Errorf("%s: the reap-vs-park guidance is missing %q", rel, m)
			}
		}
		for _, stale := range []string{
			"still leaves registration for resume",
			"still leaves the agent registered for resume",
			"stop without kill remains resume-friendly",
		} {
			if strings.Contains(lower, stale) {
				t.Errorf("%s still describes the pre-T985 park-on-done convention: %q", rel, stale)
			}
		}
	}
}
