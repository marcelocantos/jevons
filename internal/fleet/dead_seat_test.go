// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T544: only work seats (purpose work or unset) leave the registry on a
// silent death; asides, product owners and the overseer keep their rows.
func TestDeadSeatRemovable(t *testing.T) {
	for _, p := range []string{claudia.PurposeWork, "", "  "} {
		if !DeadSeatRemovable(p) {
			t.Errorf("purpose %q: want removable", p)
		}
	}
	for _, p := range []string{claudia.PurposeAside, claudia.PurposeOverseer, "product-owner"} {
		if DeadSeatRemovable(p) {
			t.Errorf("purpose %q: want kept", p)
		}
	}
}
