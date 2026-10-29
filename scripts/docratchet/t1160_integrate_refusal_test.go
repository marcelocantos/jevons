// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

func TestT1160IntegrateRefusalDoctrine(t *testing.T) {
	for _, rel := range []string{
		"internal/roles/builtin/worker.md",
		"internal/mcpserver/fleet_brief.go",
		"internal/sharedcloneseats/sharedclone.go",
	} {
		body := readRepo(t, rel)
		if !strings.Contains(body, "T1160") || !strings.Contains(body, "integrate") {
			t.Errorf("%s must cite 🎯T1160 integrate-refusal doctrine", rel)
		}
	}
}
