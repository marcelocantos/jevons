// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

// TestT5388HygieneYamlPresent ratchets 🎯T538.8 / ENT-008: the repo
// declares its hygiene floors in a root hygiene.yaml. Absence made
// T516's "hygiene.yaml declared steady-state floors" a lie; the
// validator crashed FileNotFound. Presence of the file is the
// shipped-path close. Drift of declared items is the hygiene skill's
// job, not this ratchet.
func TestT5388HygieneYamlPresent(t *testing.T) {
	body := readRepo(t, "hygiene.yaml")
	for _, m := range []string{
		"schema_version: 1",
		"repo: jevons",
		"floors:",
		"correctness:",
		"security:",
		"quality:",
		"deps:",
		"release:",
		"governance:",
		"build:",
		"docs:",
		"perf:",
		"vcs:",
		"agent:",
	} {
		if !strings.Contains(body, m) {
			t.Errorf("hygiene.yaml missing marker %q", m)
		}
	}
}
