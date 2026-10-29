// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/roles"
)

// TestT1313SandboxedFullGate pins the full instruction on every standing surface.
func TestT1313SandboxedFullGate(t *testing.T) {
	const doctrine = "When a worker's sandboxed seat cannot run its repo's full gate (nested sandbox-exec, ps or keychain refused), the fleet still gets it gated and landed without an owner step: the PO spawns an auditor (🎯T1182) on a provider whose sandbox can run that gate (🎯T1245), or a gate runner outside the worker sandbox, which runs the exact full-gate command in the worker's worktree, records it in the gate store, and integrate lands it. A GREEN gate record is valid regardless of which seat ran it. Do not park the worker or the target asking the owner to run the gate. Specimen: 2026-10-29, 🎯claudia/T237 and 🎯claudia/T238 finished with focused gates GREEN, but neither worker nor claudia-po (both sandboxed) could run claudia's full gate; claudia-po parked both and the work sat until the owner session ran the gate and integrated by hand."
	for _, file := range []string{"internal/config/persona.md", "agents-guide.md", "internal/cli/help_agent.md", "internal/mcpserver/fleet_brief.go"} {
		body := readRepo(t, file)
		if !strings.Contains(strings.Join(strings.Fields(body), " "), "## Sandboxed worker cannot run the full gate (🎯T1313) "+doctrine) {
			t.Errorf("%s: missing identical T1313 sandboxed-full-gate doctrine", file)
		}
	}
}

func folded(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// TestT1313RoleFiles pin the operational instruction on PO and auditor role bodies.
func TestT1313RoleFiles(t *testing.T) {
	po, err := roles.Builtin(roles.ProductOwner)
	if err != nil {
		t.Fatal(err)
	}
	poBody := folded(po.Body)
	for _, want := range []string{
		"🎯T1313",
		"Do not park the worker or the target asking the owner to run the gate",
		"GREEN gate record is valid regardless of which seat ran it",
	} {
		if !strings.Contains(poBody, want) {
			t.Errorf("product-owner.md missing T1313 pin %q", want)
		}
	}
	aud, err := roles.Builtin(roles.Auditor)
	if err != nil {
		t.Fatal(err)
	}
	audBody := folded(aud.Body)
	for _, want := range []string{
		"🎯T1313",
		"exact full-gate command in the worker's worktree",
		"Do not bounce the work to the owner",
	} {
		if !strings.Contains(audBody, want) {
			t.Errorf("auditor.md missing T1313 pin %q", want)
		}
	}
}
