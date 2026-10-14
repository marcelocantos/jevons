// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"regexp"
	"strings"
	"testing"
)

// TestT956TurnConfirmWindowIsNotAFixedConstantAgain ratchets the 🎯T956 fix
// against a regression back to a single fixed turn-confirm clock: the
// 45s base window (defaultTurnConfirmWindow) must stay named ALONGSIDE a
// separate, larger, named upper bound (maxTurnConfirmWindow) that the
// extension logic actually reaches for. Deleting either name, or deleting
// the extension's "while alive" / "Yes" check that reads it, silently
// re-collapses the confirm into exactly the single fixed deadline that
// released eight of nine claude-CLI mints in one round on 2026-09-30.
func TestT956TurnConfirmWindowIsNotAFixedConstantAgain(t *testing.T) {
	body := readRepo(t, "internal/mcpserver/turn_evidence.go")

	need := []string{
		"defaultTurnConfirmWindow = 45 * time.Second",
		"maxTurnConfirmWindow = 5 * time.Minute",
		"hardDeadline",
		"seatstate.Yes",
	}
	for _, m := range need {
		if !strings.Contains(body, m) {
			t.Errorf("turn_evidence.go missing 🎯T956 doctrine marker %q", m)
		}
	}

	// The two constants must differ, and the upper bound must be the
	// larger one — a ratchet that only checked presence would pass a
	// "fix" that set them equal (functionally still one fixed window).
	baseRe := regexp.MustCompile(`defaultTurnConfirmWindow = (\d+) \* time\.Second`)
	maxRe := regexp.MustCompile(`maxTurnConfirmWindow = (\d+) \* time\.Minute`)
	baseM := baseRe.FindStringSubmatch(body)
	maxM := maxRe.FindStringSubmatch(body)
	if baseM == nil || maxM == nil {
		t.Fatalf("could not locate the two confirm-window constants to compare them")
	}
	// Both patterns captured a positive integer by construction (\d+), so
	// any parse failure here is a bug in this test's regex, not input.
	if baseM[1] == maxM[1] {
		t.Fatalf("base and max windows parsed to the same literal — not a real upper bound")
	}
}

// TestT956DoctrineMarkers is a prose ratchet: the extension's reasoning
// (why a live process gets more time, why that time is still bounded, and
// what fired on 2026-09-30) must survive in the production source, not
// just in this target's filed acceptance text.
func TestT956DoctrineMarkers(t *testing.T) {
	body := readRepo(t, "internal/mcpserver/turn_evidence.go")
	need := []string{
		"🎯T956",
		"cold-start",
		"wedged",
	}
	for _, m := range need {
		if !strings.Contains(body, m) {
			t.Errorf("turn_evidence.go missing 🎯T956 doctrine marker %q", m)
		}
	}
}
