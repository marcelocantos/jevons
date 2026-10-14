// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleetintent

import "strings"

// CapacityRelatedReason reports whether a recorded intent reason reads as a
// stand-down for subscription-plan capacity (rate limits, weekly/session
// exhaustion, a provider gone hot or ahead of pace) rather than for an
// unrelated deliberate hold (an owner wanting the fleet quiet, a design
// gate, a cross-repo block).
//
// 🎯T977: a fleet-wide Parked intent recorded for capacity is exactly the
// hold a plan restoration should lift on its own. A fleet-wide Parked intent
// recorded for any other reason must not be touched by that same evidence —
// capacity headroom returning says nothing about whether the owner still
// wants the fleet quiet. This is the pure seam that tells the two apart so
// the call site (NoteCapacity) can say, in the open, which Parked reasons it
// is willing to lift.
func CapacityRelatedReason(reason string) bool {
	r := strings.ToLower(reason)
	if r == "" {
		return false
	}
	for _, kw := range capacityKeywords {
		if strings.Contains(r, kw) {
			return true
		}
	}
	return false
}

// capacityKeywords are substrings that mark a reason as capacity-related.
// Deliberately narrow: a reason naming a design gate, an owner decision, or
// a cross-repo block must not match one of these by accident.
var capacityKeywords = []string{
	"capacity",
	"rate limit",
	"rate-limit",
	"rate_limit",
	"exhausted",
	"quota",
	"weekly",
	"session limit",
	"headroom",
	"ahead of pace",
	"hot plan",
	"hot (",
	"plan is hot",
	"out of allowance",
}
