// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"

	"github.com/marcelocantos/jevons/internal/agenterr"
)

// 🎯T709: a seat sitting behind Claude Code's workspace-trust modal must
// not be re-pressured. Re-pressure is what lost ge-po on 2026-09-20 —
// the modal wants a human keypress, so every cycle re-launched into the
// same dialog and the seat was reaped with nothing owner-visible saying
// why. Failing closed here is what turns it into the owner action
// agenterr.ClassWorkspaceTrust names.
//
// The composition is the point: this asserts at the CONSUMER that the
// class's non-transience actually reaches the recover policy, not just
// that agenterr's own table says so.
func TestT709WorkspaceTrustIsNotRecoverWorthy(t *testing.T) {
	t.Parallel()
	if fleetRecoverWorthy(agenterr.ClassWorkspaceTrust) {
		t.Fatal("a modal awaiting a keypress must fail closed, not re-pressure")
	}
	// Control: the neighbouring stall class it used to be classified as
	// IS re-pressure-worthy, so this test would have passed vacuously
	// before the split.
	if !fleetRecoverWorthy(agenterr.ClassStartupStall) {
		t.Fatal("startup_stall must still be re-pressured")
	}
}
