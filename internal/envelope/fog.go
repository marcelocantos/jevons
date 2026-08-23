// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package envelope

import "strings"

// FogMap is the 🎯T536.3 fog-of-war sweep: territory the scout already
// knows, territory still unknown, and blindspots (unknown unknowns that
// should force a re-slice rather than a silent guess in code).
type FogMap struct {
	Known     []string
	Unknown   []string
	Blindspot []string
}

// HasFog is true when any fog slot was present.
func (f FogMap) HasFog() bool {
	return len(f.Known) > 0 || len(f.Unknown) > 0 || len(f.Blindspot) > 0
}

// Fog returns a copy of the message fog map.
func (m *Message) Fog() FogMap {
	if m == nil {
		return FogMap{}
	}
	return FogMap{
		Known:     append([]string(nil), m.FogKnown...),
		Unknown:   append([]string(nil), m.FogUnknown...),
		Blindspot: append([]string(nil), m.FogBlindspot...),
	}
}

// InheritLedger copies a ranked silent-decision ledger onto a spawn-brief
// so an implementer inherits the scout's pre-build decisions (🎯T536.3).
// Returns a shallow clone of from with KindSpawnBrief and PhaseImplement;
// nil when from has no ledger to inherit.
func InheritLedger(from *Message, target string) *Message {
	if from == nil || !from.HasSilentLedger() {
		return nil
	}
	out := &Message{
		Kind:         KindSpawnBrief,
		Target:       strings.TrimSpace(target),
		Phase:        PhaseImplement,
		SilentLedger: from.SilentLedger,
		Extra:        map[string]string{},
	}
	if out.Target == "" {
		out.Target = from.Target
	}
	if from.SilentLedger == SilentLedgerRanked {
		out.Decisions = append([]SilentDecision(nil), from.Decisions...)
	}
	// Carry fog forward as context the implementer can see.
	out.FogKnown = append([]string(nil), from.FogKnown...)
	out.FogUnknown = append([]string(nil), from.FogUnknown...)
	out.FogBlindspot = append([]string(nil), from.FogBlindspot...)
	return out
}

// ImplementBlockedReason reports why a scout must not advance into
// implementation (🎯T536.3). Empty means the skip rules do not block.
// Scout itself may still run; this gate is the punch-through into Build.
func ImplementBlockedReason(designGated, parkedForDesign, fuzzyRegion, hostSaturated bool) string {
	switch {
	case designGated:
		return "design-gated"
	case parkedForDesign:
		return "parked-for-design"
	case fuzzyRegion:
		return "t31.2-fuzzy"
	case hostSaturated:
		return "host-saturation-t460"
	default:
		return ""
	}
}

// MayImplementAfterScout is true when none of the standing skip rules
// block advancing from scout into implementation.
func MayImplementAfterScout(designGated, parkedForDesign, fuzzyRegion, hostSaturated bool) bool {
	return ImplementBlockedReason(designGated, parkedForDesign, fuzzyRegion, hostSaturated) == ""
}

func fogFingerprint(m *Message) string {
	if m == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("fog=")
	for _, s := range m.FogKnown {
		b.WriteString("|k:")
		b.WriteString(s)
	}
	for _, s := range m.FogUnknown {
		b.WriteString("|u:")
		b.WriteString(s)
	}
	for _, s := range m.FogBlindspot {
		b.WriteString("|b:")
		b.WriteString(s)
	}
	return b.String()
}
