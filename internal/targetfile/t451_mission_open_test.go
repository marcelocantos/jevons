// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package targetfile

import "testing"

// 🎯T451 clause 2: owner assignment closes the mission the same way
// achieved does. The 🎯T70 shape — converging + owned_by — is still
// "open" if you only read status.
func TestT451MissionOpenOwnerClosesLikeAchieved(t *testing.T) {
	t.Parallel()
	if !MissionOpen("converging", "") {
		t.Fatal("unassigned converging must stay open")
	}
	if MissionOpen("converging", "owner") {
		t.Fatal("owner-assigned converging must close — this is the T70 shape")
	}
	if MissionOpen("identified", "marcelo") {
		t.Fatal("owner-assigned identified must close")
	}
	if MissionOpen("achieved", "") {
		t.Fatal("achieved stays closed even without an owner")
	}
	if !MissionOpen("identified", "") {
		t.Fatal("unassigned identified must stay open")
	}
}
