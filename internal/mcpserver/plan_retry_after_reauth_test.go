// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"

	"github.com/marcelocantos/jevons/internal/planusage"
)

// A failed move to a destination whose login the owner just repaired stops
// reading as that failure; other results are left for the sweep.
func TestMarkPlanRetryAfterReauthClearsOnlyThatDestination(t *testing.T) {
	s := &Server{planLastResults: map[string]planusage.PlanAction{
		"jevons":  {Name: "jevons", To: "claude", Execution: "failed", Failure: "keychain item is not the plan blob"},
		"ge-po":   {Name: "ge-po", To: "cursor", Execution: "failed", Failure: "invalid_grant"},
		"claudia": {Name: "claudia", To: "claude", Execution: "migrated"},
	}}
	s.MarkPlanRetryAfterReauth("anthropic")

	if got := s.planLastResults["jevons"]; got.Execution != "pending" || got.Failure != "destination login recovered; retrying" {
		t.Fatalf("claude failure not marked retrying: %+v", got)
	}
	if got := s.planLastResults["ge-po"]; got.Execution != "failed" {
		t.Fatalf("other destination changed: %+v", got)
	}
	if got := s.planLastResults["claudia"]; got.Execution != "migrated" {
		t.Fatalf("migrated result changed: %+v", got)
	}
}
