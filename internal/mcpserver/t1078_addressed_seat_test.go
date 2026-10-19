// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T1078: a seat the owner addresses has no brief to miss, so a live one is
// running whatever the evidence says about a first turn; a work seat keeps
// 🎯T305's never_briefed and the honest phase_unknown. Liveness and a
// materialized row over located-absent records still decide for everyone.
func TestT1078AddressedSeatHasNoBriefToMiss(t *testing.T) {
	for _, tc := range []struct {
		purpose                        string
		alive, turnBegan, materialized bool
		ev                             SessionEvidence
		want                           string
	}{
		{claudia.PurposeOverseer, true, false, false, SessionEvidenceUnknown, AgentStatusRunning},
		{claudia.PurposeOverseer, true, false, false, SessionEvidenceAbsent, AgentStatusRunning},
		{claudia.PurposeAside, true, false, false, SessionEvidenceUnknown, AgentStatusRunning},
		{claudia.PurposeOverseer, false, false, false, SessionEvidenceUnknown, AgentStatusStopped},
		{claudia.PurposeOverseer, true, false, true, SessionEvidenceAbsent, AgentStatusDeadUnmaterialized},
		{claudia.PurposeWork, true, false, false, SessionEvidenceUnknown, AgentStatusPhaseUnknown},
		{claudia.PurposeWork, true, false, false, SessionEvidenceAbsent, AgentStatusNeverBriefed},
		{"", true, false, false, SessionEvidenceUnknown, AgentStatusPhaseUnknown},
	} {
		got := classifySeatListPhase(tc.purpose, tc.alive, tc.turnBegan, tc.materialized, tc.ev)
		if got != tc.want {
			t.Errorf("%+v: %s, want %s", tc, got, tc.want)
		}
	}
}
