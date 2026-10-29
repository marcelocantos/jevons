// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/roles"
)

// A product owner's auditor is its standing gate seat: a finish-report closes
// one review, not the seat. On 2026-10-22 jv-auditor-po was reaped on its own
// gate report and jevons-po's verification queue had nowhere to go. An
// auditor a worker spawned for one mission is still reaped when it finishes.
func TestPOAuditorSurvivesItsFinishReport(t *testing.T) {
	t.Parallel()
	s, reg := t985Hub(t, "jv-worker")
	for _, d := range []claudia.AgentDef{
		{Name: "jv-auditor-po", Parent: "jevons-po"},
		{Name: "jv-mission-audit", Parent: "jv-worker"},
	} {
		d.WorkDir, d.SessionID, d.Purpose, d.Role = t.TempDir(), "s-"+d.Name, claudia.PurposeWork, roles.Auditor
		d.TargetID, d.Materialized, d.Provider = "T985", true, "grok"
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	report := declaredFinishReport("T985", "Gate verified for the PO.")
	s.maybeReapDoneWorkAgent("jv-auditor-po", report)
	if reg.Def("jv-auditor-po") == nil {
		t.Fatal("the PO's standing auditor was reaped on its finish-report")
	}
	s.maybeReapDoneWorkAgent("jv-mission-audit", report)
	if reg.Def("jv-mission-audit") != nil {
		t.Fatal("a mission auditor under a worker stayed registered after its finish-report")
	}
}
