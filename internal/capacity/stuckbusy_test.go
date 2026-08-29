// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package capacity

import "testing"

func TestT567StuckBusyScaleFollowsLoadPerCore(t *testing.T) {
	cases := []struct {
		load  float64
		cores int
		want  float64
	}{
		{0, 16, 1},  // unknown load never stretches
		{8, 0, 1},   // unknown cores never stretches
		{8, 16, 1},  // half a core each: normal host
		{16, 16, 1}, // one per core: still normal
		{48, 16, 3}, // 3×/core → 3× threshold
		{300, 16, MaxStuckBusyScale},
	}
	for _, c := range cases {
		if got := StuckBusyScale(c.load, c.cores); got != c.want {
			t.Errorf("StuckBusyScale(%v, %d) = %v, want %v", c.load, c.cores, got, c.want)
		}
	}
}
