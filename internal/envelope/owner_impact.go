// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package envelope

import "strings"

// OwnerImpact is the finish-report slot answering 🎯T1122 clause 3: what the
// owner does differently because of an owner-visible change. Mirrors the
// owner-reply purpose slot (🎯T1140) — a closed vocabulary, absent and
// explicit "none" both read as no claimed impact.
type OwnerImpact string

const (
	// OwnerImpactNone is an absent slot.
	OwnerImpactNone OwnerImpact = ""
	// OwnerImpactNoneSlot is an explicit "none" — the author looked and
	// found nothing the owner must act on.
	OwnerImpactNoneSlot OwnerImpact = "none"
	// OwnerImpactDecision: the owner must make a call because of this.
	OwnerImpactDecision OwnerImpact = "decision"
	// OwnerImpactFault: the owner must know about a defect because of this.
	OwnerImpactFault OwnerImpact = "fault"
	// OwnerImpactPlanChange: what the owner would do next changes because
	// of this.
	OwnerImpactPlanChange OwnerImpact = "plan-change"
)

// allOwnerImpacts is every non-absent value the slot accepts, in the order a
// correction notice lists them.
var allOwnerImpacts = []OwnerImpact{
	OwnerImpactNoneSlot,
	OwnerImpactDecision,
	OwnerImpactFault,
	OwnerImpactPlanChange,
}

// ParseOwnerImpact maps a slot value onto OwnerImpact. Unknown names fail.
func ParseOwnerImpact(raw string) (OwnerImpact, bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.Join(strings.Fields(s), "-")
	if s == "" {
		return OwnerImpactNone, true
	}
	p := OwnerImpact(s)
	for _, known := range allOwnerImpacts {
		if p == known {
			return p, true
		}
	}
	return "", false
}

func (o OwnerImpact) String() string { return string(o) }

// Declared is true when the slot names an actual owner impact (decision,
// fault, or plan-change) — not absent, and not an explicit "none" (🎯T1122).
func (o OwnerImpact) Declared() bool {
	switch o {
	case OwnerImpactDecision, OwnerImpactFault, OwnerImpactPlanChange:
		return true
	default:
		return false
	}
}

func ownerImpactSlotError(value string) *SlotError {
	accepted := make([]string, len(allOwnerImpacts))
	for i, v := range allOwnerImpacts {
		accepted[i] = string(v)
	}
	return &SlotError{
		Slot:     "owner-impact",
		Value:    value,
		Accepted: accepted,
		Hint:     "name what the owner does differently: decision | fault | plan-change — or none when nothing owner-visible changed (🎯T1122)",
	}
}
