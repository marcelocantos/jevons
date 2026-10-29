// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/roles"
)

// TestT511FleetBriefNoRoleConditionalProse ratchets 🎯T511: role-conditional
// "if you are …" doctrine is not duplicated in the universal standing brief.
func TestT511FleetBriefNoRoleConditionalProse(t *testing.T) {
	brief := readRepo(t, "internal/mcpserver/fleet_brief.go")
	lower := strings.ToLower(brief)
	for _, banned := range []string{
		"if you are a stratum-1 product owner",
		"if you are an auditor",
		"you are spawn-only for build work",
	} {
		if strings.Contains(lower, banned) {
			t.Errorf("FleetStandingBrief still carries role-conditional prose %q", banned)
		}
	}
	if !strings.Contains(brief, "Role doctrine injection (🎯T511)") {
		t.Error("FleetStandingBrief missing T511 injection pointer section")
	}
}

// TestT511RoleFilesAuthoritative ratchets built-in role bodies for core fleet roles.
func TestT511RoleFilesAuthoritative(t *testing.T) {
	po, err := roles.Builtin(roles.ProductOwner)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"🎯T125", "🎯T1182", "🎯T1313", "🎯T155", "🎯T193", "🎯T325.1"} {
		if !strings.Contains(po.Body, want) {
			t.Errorf("product-owner.md missing %q", want)
		}
	}
	ov, err := roles.Builtin(roles.Overseer)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"🎯T129", "🎯T1053", "🎯T1140", "🎯T31", "🎯T427", "🎯T692", "🎯T176"} {
		if !strings.Contains(ov.Body, want) {
			t.Errorf("overseer.md missing %q", want)
		}
	}
	wk, err := roles.Builtin(roles.Worker)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(wk.Body, "🎯T125 is **not** yours") {
		t.Error("worker.md missing inverted T125 bound")
	}
}

// TestT511IdentityHeaderPointsAtRoleFile ratchets identity_header.go for the
// role-file pointer without inlined doctrine.
func TestT511IdentityHeaderPointsAtRoleFile(t *testing.T) {
	src := readRepo(t, "internal/mcpserver/identity_header.go")
	if strings.Contains(src, "roleAddressedDoctrine") {
		t.Error("identity_header.go still defines roleAddressedDoctrine (🎯T511)")
	}
	for _, want := range []string{"RoleDoc", "ROLE DOCTRINE:", "roles.RepoRelPath"} {
		if !strings.Contains(src, want) {
			t.Errorf("identity_header.go missing %q", want)
		}
	}
}

// TestT511AgentsGuideDocumentsRoles ratchets agents-guide.md (and its
// help_agent.md mirror) for the fleet-role mechanism — not only T536.2 auditor.
func TestT511AgentsGuideDocumentsRoles(t *testing.T) {
	for _, rel := range []string{"agents-guide.md", "internal/cli/help_agent.md"} {
		guide := readRepo(t, rel)
		for _, want := range []string{
			"Fleet agent roles (🎯T511)",
			"internal/roles/builtin",
			"internal/config/roles",
			"owner → repo → built-in",
			"agent_roles.json",
			"jevons_agent_start",
			"role=",
			"[Jevons role doctrine]",
			"ROLE DOCTRINE",
			"roles.Assemble",
			"docs/design/t511-fleet-brief-buckets.md",
		} {
			if !strings.Contains(guide, want) {
				t.Errorf("%s missing T511 marker %q", rel, want)
			}
		}
	}
}
