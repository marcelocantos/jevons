// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import "testing"

// One placement notice per author per cooldown. Each notice opens a turn, and
// jevons-po answered each one with another misplaced report (2026-10-23).
func TestPlacementCorrectionIsRateLimitedPerAuthor(t *testing.T) {
	s := &Server{}
	misplaced := func(word string) string {
		return "Summary " + word + ".\n\n```jevons\njevons: kind finish-report\njevons: target T1\njevons: oracle sha=2ca7a7d4186c\njevons: silent-ledger none\n```\n\nMore prose after the fence.\n"
	}
	if !s.tryDeliverPlacementCorrection("jevons-po", misplaced("one")) {
		t.Fatal("first misplaced report got no notice")
	}
	if s.tryDeliverPlacementCorrection("jevons-po", misplaced("two")) {
		t.Fatal("a second, different misplaced report inside the cooldown got another notice")
	}
	if !s.tryDeliverPlacementCorrection("jv-worker", misplaced("three")) {
		t.Fatal("another author's first misplaced report was suppressed")
	}
}
