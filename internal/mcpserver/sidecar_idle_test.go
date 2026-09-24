// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"

	"github.com/marcelocantos/claudia"
)

func TestSidecarSeatWithoutClaudeJSONLIsNotIdle(t *testing.T) {
	ev := ReadSessionEvidence(claudia.ProviderCursor, "no-such-session", t.TempDir())
	if ev != SessionEvidenceUnknown {
		t.Fatalf("sidecar evidence = %v, want unknown (not absent/idle)", ev)
	}
	phase := ClassifyAgentPhase(true, false, true, ev)
	if phase == AgentStatusDeadUnmaterialized || phase == "idle" {
		t.Fatalf("sidecar with no Claude JSONL classified %q", phase)
	}
}
