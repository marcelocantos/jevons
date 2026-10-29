// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

// 🎯T1151: the design note maps each 2026-10-21/22 incident class to the
// property that removes it (umbrella acceptance, not only 🎯T1151.5 scout).
func TestT1151DesignNoteMapsIncidentClasses(t *testing.T) {
	doc := readRepo(t, "docs/design/fleet-shared-state.md")
	lower := strings.ToLower(doc)
	for _, id := range []string{"t1134", "t1144", "t1145", "t1146", "t1147", "t1141"} {
		if !strings.Contains(lower, id) {
			t.Errorf("fleet-shared-state.md missing incident 🎯%s", strings.ToUpper(id))
		}
	}
	for _, child := range []string{"t1151.1", "t1151.2", "t1151.3", "t1151.4"} {
		if !strings.Contains(lower, child) {
			t.Errorf("fleet-shared-state.md missing property child 🎯%s", strings.ToUpper(child))
		}
	}
	if !strings.Contains(doc, "Incident map") {
		t.Error("fleet-shared-state.md missing incident map section")
	}
}
