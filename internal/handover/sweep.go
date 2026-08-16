// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package handover

import (
	"fmt"
	"time"
)

// How long a pending seed may sit without a live process before the
// sweep stops waiting for "the next launch" and tells someone.
const DefaultHandoverStale = 15 * time.Minute

// HandoverAction is what SweepHandovers should do with one record.
type HandoverAction string

const (
	HandoverRetry   HandoverAction = "retry"
	HandoverSurface HandoverAction = "surface"
	HandoverReap    HandoverAction = "reap"
	HandoverWait    HandoverAction = "wait"
)

// ClassifyHandover is the 🎯T418 clause 5 policy. Pending is a state
// with an owner and a clock: retry when a live process can take the
// seed, surface when nobody will launch, reap when the seat is gone
// or the seed already landed.
func ClassifyHandover(p Pending, now time.Time, inRegistry, alive bool) (HandoverAction, string) {
	age, hasAge := p.Age(now)
	ageNote := p.DescribeAge(now)
	switch {
	case p.Delivered:
		return HandoverReap, "seed already marked delivered"
	case !inRegistry:
		return HandoverReap, fmt.Sprintf("agent left the registry (%s)", ageNote)
	case !p.Usable():
		return HandoverSurface, fmt.Sprintf("record cannot seed a successor (%s)", ageNote)
	case alive:
		return HandoverRetry, "live process can take the seed"
	case !hasAge:
		return HandoverSurface, "no readable created_at — cannot wait on an unknown clock"
	case age >= DefaultHandoverStale:
		return HandoverSurface, fmt.Sprintf("pending %s with no live process — a launch that may never come is not a retry", ageNote)
	default:
		return HandoverWait, fmt.Sprintf("no live process yet; waiting %s before surfacing", ageNote)
	}
}
