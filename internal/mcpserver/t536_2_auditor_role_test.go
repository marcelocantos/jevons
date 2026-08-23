// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/roles"
)

func TestT5362SpawnAuditorRecordsRoleAndAssemblesDoctrine(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	if err := s.OpenRoleAssignments(dir); err != nil {
		t.Fatal(err)
	}

	s.mu.Lock()
	s.pendingSpawnRole = roles.Auditor
	s.mu.Unlock()
	def, existed, _, err := s.stitchAgentStart(
		"jv-t536.2-audit", dir, "", "", "",
		"jevons-po", claudia.PurposeWork, "T536.2", "",
	)
	s.mu.Lock()
	s.pendingSpawnRole = ""
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if existed {
		t.Fatal("expected mint")
	}
	if got := s.roleDisplay(*def); got != roles.Auditor {
		t.Fatalf("roleDisplay=%q", got)
	}
	if got := s.agentRole(def.Name, def.Purpose); got != roles.Auditor {
		t.Fatalf("assignments=%q", got)
	}

	rdef, err := s.resolveRoleDef(roles.Auditor)
	if err != nil {
		t.Fatal(err)
	}
	assembled := roles.Assemble(FleetStandingBrief, rdef.Body, "Review the ledger.")
	for _, want := range []string{
		"Jevons fleet standing brief",
		"[Jevons role doctrine]",
		"cannot write or patch product code",
		"silent-decision ledger",
		"Review the ledger.",
	} {
		if !strings.Contains(assembled, want) {
			t.Errorf("assembly missing %q", want)
		}
	}

	if strings.Contains(FleetStandingBrief, "if you are an auditor") ||
		strings.Contains(strings.ToLower(FleetStandingBrief), "role=auditor") {
		t.Fatal("FleetStandingBrief must not carry auditor if-you-are doctrine")
	}

	hdr := s.identityHeaderFor(def.Name)
	if !strings.Contains(hdr, "auditor") || !strings.Contains(hdr, "silent-decision ledger") {
		t.Fatalf("auditor identity header:\n%s", hdr)
	}

	if err := (roles.Catalog{}).Delete(roles.Auditor, 0, false); err == nil {
		t.Fatal("expected builtin delete refusal")
	}
}
