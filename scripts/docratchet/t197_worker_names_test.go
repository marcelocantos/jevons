// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package docratchet holds hermetic prose/inventory ratchets.
// 🎯T197: hierarchical fleet worker names keep literal dots.
package docratchet_test

import (
	"strings"
	"testing"
)

// TestT197WorkerNamesLiteralDotsDoctrine ratchets the load-bearing examples
// and anti-squash policy across doctrine surfaces (not a live spawn).
func TestT197WorkerNamesLiteralDotsDoctrine(t *testing.T) {
	surfaces := []struct {
		path string
	}{
		{"agents-guide.md"},
		{"AGENTS.md"},
		{"internal/config/persona.md"},
		{"internal/mcpserver/fleet_brief.go"},
	}
	need := []string{
		"T197",
		"jv-t27.2-config",
		"jv-t272-config",
		"digit-squash",
		"jv-t159-seal",
		"literal dots",
	}
	for _, s := range surfaces {
		body := readRepo(t, s.path)
		for _, m := range need {
			if !strings.Contains(body, m) {
				t.Errorf("%s missing T197 marker %q", s.path, m)
			}
		}
	}
	// Spawn tool description carries the policy example (product path).
	agentsSrc := readRepo(t, "internal/mcpserver/agents.go")
	for _, m := range []string{
		"jv-t27.2-config",
		"jv-t272-config",
		"T197",
		"literal dots",
	} {
		if !strings.Contains(agentsSrc, m) {
			t.Errorf("agents.go tool surface missing T197 marker %q", m)
		}
	}
}
