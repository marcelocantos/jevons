// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package envelope

import "strings"

// Phase is the mission phase on a spawn-brief or scout/finish handoff
// (🎯T536.3). Distinguishes fog-of-war scout from implementation.
type Phase string

const (
	// PhaseNone means the slot was omitted. Readers treat spawn-brief as
	// implement for backward compatibility unless Kind is scout-report.
	PhaseNone Phase = ""
	// PhaseScout is a fog-of-war / known-unknown-blindspot pass before
	// implementation. Must not punch through design-gated / parked /
	// T31.2-fuzzy / host-saturated (T460) leaves into implement.
	PhaseScout Phase = "scout"
	// PhaseImplement is ordinary Build work. May inherit a scout ledger.
	PhaseImplement Phase = "implement"
)

// ParsePhase maps a slot value onto Phase. Unknown names fail.
func ParsePhase(raw string) (Phase, bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.ReplaceAll(s, "_", "-")
	switch s {
	case "", "none":
		return PhaseNone, true
	case "scout", "fog", "fog-of-war", "fogofwar":
		return PhaseScout, true
	case "implement", "build", "impl":
		return PhaseImplement, true
	default:
		return "", false
	}
}

func (p Phase) String() string { return string(p) }

// IsScout is true for the explicit scout phase slot.
func (p Phase) IsScout() bool { return p == PhaseScout }

// IsImplement is true for the explicit implement phase slot.
func (p Phase) IsImplement() bool { return p == PhaseImplement }

// EffectivePhase resolves omitted phase from kind: scout-report ⇒ scout,
// otherwise implement (spawn-brief default).
func EffectivePhase(m *Message) Phase {
	if m == nil {
		return PhaseImplement
	}
	if m.Phase != PhaseNone {
		return m.Phase
	}
	if m.Kind == KindScoutReport {
		return PhaseScout
	}
	return PhaseImplement
}

// IsScoutMission is true when the envelope is a scout-phase spawn or
// a scout-report handoff.
func IsScoutMission(m *Message) bool {
	return EffectivePhase(m) == PhaseScout
}
