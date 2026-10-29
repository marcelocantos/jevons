// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

func TestT358FableDefaultsDecisionPacket(t *testing.T) {
	body := readRepo(t, "docs/design/t358-fable-class-defaults-owner-decision.md")
	for _, m := range []string{
		"Root overseer",
		"Product owners",
		"Workers",
		"keep_current",
		"root_fable",
		"po_fable",
		"both",
		"tiered",
		"fable_class_defaults",
		"does **not** achieve 🎯T358",
		"🎯T392.3",
	} {
		if !strings.Contains(body, m) {
			t.Errorf("packet missing %q", m)
		}
	}
}
