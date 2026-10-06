// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleetintent

import "testing"

// 🎯T977: CapacityRelatedReason is the pure seam that decides whether a
// fleet-wide Parked intent was a stand-down for capacity (safe to lift on
// restoration) or something else entirely (never touched by that evidence).
func TestT977CapacityRelatedReason(t *testing.T) {
	cases := []struct {
		reason string
		want   bool
	}{
		{"", false},
		{"owner wants the fleet quiet for the weekend", false},
		{"design gate: 🎯T112 needs owner taste before implement", false},
		{"cross-repo block: waiting on sibling repo release", false},
		{"claude weekly hot (92% used), standing down for capacity", true},
		{"rate limit wave across all providers", true},
		{"provider hard-block: spend wall", false}, // T406 is a different hold, not capacity
		{"codex session exhausted", true},
		{"every plan ahead of pace or exhausted; parking until headroom returns", true},
		{"ClAuDe QUOTA gone", true},
	}
	for _, c := range cases {
		if got := CapacityRelatedReason(c.reason); got != c.want {
			t.Errorf("CapacityRelatedReason(%q) = %v, want %v", c.reason, got, c.want)
		}
	}
}
