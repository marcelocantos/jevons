// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

// TestT664UncertainVerdictIsResolvedByReadingNotStopping ratchets the 🎯T664
// doctrine on every instruction surface that describes the send verdicts:
// delivered_unconfirmed is resolved by reading, and never by stopping, killing
// or re-sending. The 2026-09-15 stopped-worker shape was an overseer that read
// "treat as undelivered until the agent acts" as licence to stop the seat.
func TestT664UncertainVerdictIsResolvedByReadingNotStopping(t *testing.T) {
	for _, rel := range []string{
		"internal/config/persona.md",
		"agents-guide.md",
		"internal/cli/help_agent.md",
		"internal/mcpserver/fleet_brief.go",
	} {
		body := readRepo(t, rel)
		if !strings.Contains(body, "delivered_unconfirmed") {
			t.Errorf("%s no longer describes the delivered_unconfirmed verdict", rel)
			continue
		}
		lower := strings.ToLower(body)
		for _, m := range []string{"never stop, kill or re-send", "🎯t664", "jevons_transcript_read"} {
			if !strings.Contains(lower, m) {
				t.Errorf("%s: the delivered_unconfirmed guidance is missing %q", rel, m)
			}
		}
		if strings.Contains(lower, "treat as undelivered until the agent acts") ||
			strings.Contains(lower, "treat as **undelivered** until") {
			t.Errorf("%s still says to treat delivered_unconfirmed as undelivered until the agent acts — that reading stopped seats", rel)
		}
	}
}
